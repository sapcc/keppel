/*******************************************************************************
*
* Copyright 2021 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package clairintegration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/gofrs/uuid"
	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/respondwith"
	"golang.org/x/exp/slices"

	"github.com/sapcc/keppel/internal/clair"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/processor"
)

// API contains state variables used by the Clair API proxy.
type API struct {
	cfg keppel.Configuration
	ad  keppel.AuthDriver
	db  *keppel.DB

	//non-pure functions that can be replaced by deterministic doubles for unit tests
	timeNow func() time.Time
}

// NewAPI constructs a new API instance.
func NewAPI(cfg keppel.Configuration, ad keppel.AuthDriver, db *keppel.DB) *API {
	return &API{cfg, ad, db, time.Now}
}

// OverrideTimeNow replaces time.Now with a test double.
func (a *API) OverrideTimeNow(timeNow func() time.Time) *API {
	a.timeNow = timeNow
	return a
}

func (a *API) processor() *processor.Processor {
	return processor.New(a.cfg, a.db, nil, nil, nil)
}

// AddTo implements the api.API interface.
func (a *API) AddTo(r *mux.Router) {
	if a.cfg.ClairClient != nil {
		r.Methods("POST").Path("/clair-notification").HandlerFunc(a.clairNotification)
		r.Methods("GET", "HEAD").Path("/clair/{path:.+}").HandlerFunc(a.reverseProxyToClair)
	}
}

func (a *API) reverseProxyToClair(w http.ResponseWriter, r *http.Request) {
	uid, authErr := a.ad.AuthenticateUserFromRequest(r)
	if authErr != nil {
		authErr.WriteAsTextTo(w)
		w.Write([]byte("\n"))
		return
	}

	if uid == nil || !uid.HasPermission(keppel.CanAdministrateKeppel, "") {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	responseBody := make(map[string]interface{})
	err := a.cfg.ClairClient.SendRequest(r.Method, mux.Vars(r)["path"], &responseBody)
	//We could put much more effort into reverse-proxying error responses
	//properly, but since this interface is only intended for when an admin
	//wants to double-check a Clair API response manually, it's okay to just
	//make every error a 500 and give the actual reason in the error message.
	if respondwith.ErrorText(w, err) {
		return
	}

	respondwith.JSON(w, http.StatusOK, responseBody)
}

// based on https://github.com/quay/clair/blob/main/notifier/callback.go //
type Callback struct {
	NotificationID uuid.UUID `json:"notification_id"`
	Callback       url.URL   `json:"callback"`
}

func (cb *Callback) UnmarshalJSON(b []byte) error {
	var m = make(map[string]string, 2)
	err := json.Unmarshal(b, &m)
	if err != nil {
		return err
	}
	if _, ok := m["notification_id"]; !ok {
		return fmt.Errorf("json unmarshal failed. webhook requires a \"notification_id\" field")
	}
	if _, ok := m["callback"]; !ok {
		return fmt.Errorf("json unmarshal failed. webhook requires a \"callback\" field")
	}

	uid, err := uuid.FromString(m["notification_id"])
	if err != nil {
		return fmt.Errorf("json unmarshal failed. malformed notification uuid: %v", err)
	}
	cbURL, err := url.Parse(m["callback"])
	if err != nil {
		return fmt.Errorf("json unmarshal failed. malformed callback url: %v", err)
	}

	(*cb).NotificationID = uid
	(*cb).Callback = *cbURL
	return nil
}

// end //

func (a *API) clairNotification(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/clair-notification")

	c := a.cfg.ClairClient

	// retrieve and parse notification
	secretHeader := r.Header[http.CanonicalHeaderKey("X-KEPPEL-CLAIR-NOTIFICATION-SECRET")]
	if !slices.Contains(secretHeader, c.NotificationSecret) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	reqBodyBytes, err := io.ReadAll(r.Body)
	if respondwith.ErrorText(w, err) {
		return
	}

	var notificationCallback Callback
	err = keppel.JSONUnmarshalStrict(reqBodyBytes, &notificationCallback)
	if respondwith.ErrorText(w, err) {
		return
	}

	// from now on no longer report errors to the clair notifier as the notification has been delivered
	// but the handling may fail due to potential various unrelated problems
	err = a.handleClairNotifications(r.Context(), notificationCallback.Callback.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		logg.Error(err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (a *API) handleClairNotifications(ctx context.Context, callbackPath string) error {
	// TODO: as an optimisation we could parse the vulnerability if the reason is added
	// https://quay.github.io/clair/reference/api.html?search=state#:~:text=to%20continue%20paging-,PagedNotifications,-%7B%0A%20%20%22page%22

	c := a.cfg.ClairClient

	// collect all notifications
	var notifications []clair.Notification
	notificationResponse, err := c.GetNotification(callbackPath, "")
	if err != nil {
		return err
	}
	notifications = append(notifications, notificationResponse.Notifications...)

	for notificationResponse.Page.Next != nil && notificationResponse.Page.Next.String() != "" {
		notificationResponse, err = c.GetNotification(callbackPath, "")
		if err != nil {
			return err
		}

		notifications = append(notifications, notificationResponse.Notifications...)
	}

	// process and delete the collected notifications
	var notificationsToDelete []string
	for _, notification := range notifications {
		err := a.processor().OverrideTimeNow(a.timeNow).SetManifestAndParentsToPending(ctx, notification.ManifestDigest)
		if err != nil {
			return err
		}

		notificationsToDelete = append(notificationsToDelete, notification.ID)
	}

	// delete notification id to free up resources faster
	for _, id := range notificationsToDelete {
		err = c.DeleteNotification(id)
		// notifications don't must be deleted but it frees up resources
		logg.Error("error while trying to delete notification: %s", err.Error())
	}

	return nil
}
