/******************************************************************************
*
*  Copyright 2018 Stefan Majewsky <majewsky@gmx.net>
*
*  Licensed under the Apache License, Version 2.0 (the "License");
*  you may not use this file except in compliance with the License.
*  You may obtain a copy of the License at
*
*      http://www.apache.org/licenses/LICENSE-2.0
*
*  Unless required by applicable law or agreed to in writing, software
*  distributed under the License is distributed on an "AS IS" BASIS,
*  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*  See the License for the specific language governing permissions and
*  limitations under the License.
*
******************************************************************************/

package schwift

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

//Object represents a Swift object. Instances are usually obtained by
//traversing downwards from a container with Container.Object() or
//Container.Objects().
type Object struct {
	c    *Container
	name string
	//cache
	headers        *ObjectHeaders //from HEAD/GET without ?symlink=get
	symlinkHeaders *ObjectHeaders //from HEAD/GET with ?symlink=get
}

//IsEqualTo returns true if both Object instances refer to the same object.
func (o *Object) IsEqualTo(other *Object) bool {
	return other.name == o.name && other.c.IsEqualTo(o.c)
}

//Object returns a handle to the object with the given name within this
//container. This function does not issue any HTTP requests, and therefore cannot
//ensure that the object exists. Use the Exists() function to check for the
//object's existence.
func (c *Container) Object(name string) *Object {
	return &Object{c: c, name: name}
}

//Container returns a handle to the container this object is stored in.
func (o *Object) Container() *Container {
	return o.c
}

//Name returns the object name. This does not parse the name in any way; if you
//want only the basename portion of the object name, use package path from the
//standard library in conjunction with this function. For example:
//
//	obj := account.Container("docs").Object("2018-02-10/invoice.pdf")
//	obj.Name()            //returns "2018-02-10/invoice.pdf"
//	path.Base(obj.Name()) //returns            "invoice.pdf"
func (o *Object) Name() string {
	return o.name
}

//FullName returns the container name and object name joined together with a
//slash. This identifier is used by Swift in several places (large object
//manifests, symlink targets, etc.) to refer to an object within an account.
//For example:
//
//	obj := account.Container("docs").Object("2018-02-10/invoice.pdf")
//	obj.Name()     //returns      "2018-02-10/invoice.pdf"
//	obj.FullName() //returns "docs/2018-02-10/invoice.pdf"
func (o *Object) FullName() string {
	return o.c.name + "/" + o.name
}

//Exists checks if this object exists, potentially by issuing a HEAD request
//if no Headers() have been cached yet.
func (o *Object) Exists() (bool, error) {
	_, err := o.Headers()
	if Is(err, http.StatusNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

//Headers returns the ObjectHeaders for this object. If the ObjectHeaders
//has not been cached yet, a HEAD request is issued on the object.
//
//For symlinks, this operation returns the metadata for the target object. Use
//Object.SymlinkHeaders() to obtain the metadata for the symlink instead.
//
//This operation fails with http.StatusNotFound if the object does not exist.
func (o *Object) Headers() (ObjectHeaders, error) {
	if o.headers != nil {
		return *o.headers, nil
	}

	hdr, err := o.fetchHeaders(nil)
	if err != nil {
		return ObjectHeaders{}, err
	}
	o.headers = hdr
	return *hdr, nil
}

func (o *Object) fetchHeaders(opts *RequestOptions) (*ObjectHeaders, error) {
	resp, err := Request{
		Method:        "HEAD",
		ContainerName: o.c.name,
		ObjectName:    o.name,
		Options:       opts,
		//since Openstack LOVES to be inconsistent with everything (incl. itself),
		//this returns 200 instead of 204
		ExpectStatusCodes: []int{200},
		DrainResponseBody: true,
	}.Do(o.c.a.backend)
	if err != nil {
		return nil, err
	}

	headers := ObjectHeaders{headersFromHTTP(resp.Header)}
	return &headers, headers.Validate()
}

//Update updates the object's headers using a POST request. To add URL
//parameters, pass a non-nil *RequestOptions.
//
//This operation fails with http.StatusNotFound if the object does not exist.
//
//A successful POST request implies Invalidate() since it may change metadata.
func (o *Object) Update(headers ObjectHeaders, opts *RequestOptions) error {
	_, err := Request{
		Method:            "POST",
		ContainerName:     o.c.name,
		ObjectName:        o.name,
		Options:           cloneRequestOptions(opts, headers.Headers),
		ExpectStatusCodes: []int{202},
	}.Do(o.c.a.backend)
	if err == nil {
		o.Invalidate()
	}
	return err
}

//UploadOptions invokes advanced behavior in the Object.Upload() method.
type UploadOptions struct {
	//When overwriting a large object, delete its segments. This will cause
	//Upload() to call into BulkDelete(), so a BulkError may be returned.
	DeleteSegments bool
}

//Upload creates the object using a PUT request.
//
//If you do not have an io.Reader, but you have a []byte or string instance
//containing the object, wrap it in a *bytes.Reader instance like so:
//
//	var buffer []byte
//	o.Upload(bytes.NewReader(buffer), opts)
//
//	//or...
//	var buffer string
//	o.Upload(bytes.NewReader([]byte(buffer)), opts)
//
//If you have neither an io.Reader nor a []byte or string, but you have a
//function that generates the object's content into an io.Writer, use
//UploadWithWriter instead.
//
//If the object is very large and you want to upload it in segments, use
//LargeObject.Append() instead. See documentation on type LargeObject for
//details.
//
//If content is a *bytes.Reader or a *bytes.Buffer instance, the Content-Length
//and Etag request headers will be computed automatically. Otherwise, it is
//highly recommended that the caller set these headers (if possible) to allow
//the server to check the integrity of the uploaded file.
//
//If Etag and/or Content-Length is supplied and the content does not match
//these parameters, http.StatusUnprocessableEntity is returned. If Etag is not
//supplied and cannot be computed in advance, Upload() will compute the Etag as
//data is read from the io.Reader, and compare the result to the Etag returned
//by Swift, returning ErrChecksumMismatch in case of mismatch. The object will
//have been uploaded at that point, so you will usually want to Delete() it.
//
//This function can be used regardless of whether the object exists or not.
//
//A successful PUT request implies Invalidate() since it may change metadata.
func (o *Object) Upload(content io.Reader, opts *UploadOptions, ropts *RequestOptions) error {
	if opts == nil {
		opts = &UploadOptions{}
	}

	ropts = cloneRequestOptions(ropts, nil)
	hdr := ObjectHeaders{ropts.Headers}

	if !hdr.SizeBytes().Exists() {
		value := tryComputeContentLength(content)
		if value != nil {
			hdr.SizeBytes().Set(*value)
		}
	}

	//do not attempt to add the Etag header when we're writing a large object
	//manifest; the header refers to the content, but we would be computing the
	//manifest's hash instead
	isManifestUpload := ropts.Values.Get("multipart-manifest") == "put" || hdr.IsDynamicLargeObject()

	var hasher hash.Hash
	if !isManifestUpload {
		tryComputeEtag(content, hdr)

		//could not compute Etag in advance -> need to check on the fly
		if !hdr.Etag().Exists() {
			hasher = md5.New()
			if content != nil {
				content = io.TeeReader(content, hasher)
			}
		}
	}

	var lo *LargeObject
	if opts.DeleteSegments {
		//enumerate segments in large object before overwriting it, but only delete
		//the segments after successfully uploading the new object to decrease the
		//chance of an inconsistent state following an upload error
		var err error
		lo, err = o.AsLargeObject()
		switch err {
		case nil:
			//okay, delete segments at the end
		case ErrNotLarge:
			//okay, do not try to delete segments
			lo = nil
		default:
			//unexpected error
			return err
		}
	}

	resp, err := Request{
		Method:            "PUT",
		ContainerName:     o.c.name,
		ObjectName:        o.name,
		Options:           ropts,
		Body:              content,
		ExpectStatusCodes: []int{201},
		DrainResponseBody: true,
	}.Do(o.c.a.backend)
	if err != nil {
		return err
	}
	o.Invalidate()

	if hasher != nil {
		expectedEtag := hex.EncodeToString(hasher.Sum(nil))
		if expectedEtag != resp.Header.Get("Etag") {
			return ErrChecksumMismatch
		}
	}

	if opts.DeleteSegments && lo != nil {
		_, _, err := lo.object.c.a.BulkDelete(lo.SegmentObjects(), nil, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

type readerWithLen interface {
	//Returns the number of bytes in the unread portion of the buffer.
	//Implemented by bytes.Reader, bytes.Buffer and strings.Reader.
	Len() int
}

func tryComputeContentLength(content io.Reader) *uint64 {
	if content == nil {
		val := uint64(0)
		return &val
	} else if r, ok := content.(readerWithLen); ok {
		val := uint64(r.Len())
		return &val
	}
	return nil
}

func tryComputeEtag(content io.Reader, headers ObjectHeaders) {
	h := headers.Etag()
	if h.Exists() {
		return
	}
	switch r := content.(type) {
	case nil:
		sum := md5.Sum(nil)
		h.Set(hex.EncodeToString(sum[:]))
	case *bytes.Buffer:
		//bytes.Buffer has a method that returns the unread portion of the buffer,
		//so this one is easy
		sum := md5.Sum(r.Bytes())
		h.Set(hex.EncodeToString(sum[:]))
	case io.ReadSeeker:
		//bytes.Reader does not have such a method, but it is an io.Seeker, so we
		//can read the entire thing and then seek back to where we started
		hash := md5.New()
		n, _ := io.Copy(hash, r)
		r.Seek(-n, io.SeekCurrent)
		h.Set(hex.EncodeToString(hash.Sum(nil)))
	}
}

//UploadWithWriter is a variant of Upload that can be used when the object's
//content is generated by some function or package that takes an io.Writer
//instead of supplying an io.Reader. For example:
//
//	func greeting(target io.Writer, name string) error {
//	    _, err := fmt.Fprintf(target, "Hello %s!\n", name)
//	    return err
//	}
//
//	obj := container.Object("greeting-for-susan-and-jeffrey")
//	err := obj.UploadWithWriter(nil, func(w io.Writer) error {
//	    err := greeting(w, "Susan")
//	    if err == nil {
//	        err = greeting(w, "Jeffrey")
//	    }
//	    return err
//	})
//
//If you do not need an io.Writer, always use Upload instead.
//
//TODO rename to UploadViaWriter
func (o *Object) UploadWithWriter(opts *UploadOptions, ropts *RequestOptions, callback func(io.Writer) error) error {
	reader, writer := io.Pipe()
	errChan := make(chan error)
	go func() {
		err := o.Upload(reader, opts, ropts)
		reader.CloseWithError(err) //stop the writer if it is still writing
		errChan <- err
	}()
	writer.CloseWithError(callback(writer)) //stop the reader if it is still reading
	return <-errChan
}

//DeleteOptions invokes advanced behavior in the Object.Delete() method.
type DeleteOptions struct {
	//When deleting a large object, also delete its segments. This will cause
	//Delete() to call into BulkDelete(), so a BulkError may be returned.
	DeleteSegments bool
}

//Delete deletes the object using a DELETE request. To add URL parameters,
//pass a non-nil *RequestOptions.
//
//This operation fails with http.StatusNotFound if the object does not exist.
//
//A successful DELETE request implies Invalidate().
func (o *Object) Delete(opts *DeleteOptions, ropts *RequestOptions) error {
	if opts == nil {
		opts = &DeleteOptions{}
	}
	if opts.DeleteSegments {
		exists, err := o.Exists()
		if err != nil {
			return err
		}
		if exists {
			lo, err := o.AsLargeObject()
			switch err {
			case nil:
				//is large object - delete segments and the object itself in one step
				_, _, err := o.c.a.BulkDelete(append(lo.SegmentObjects(), o), nil, nil)
				o.Invalidate()
				return err
			case ErrNotLarge:
				//not a large object - use regular DELETE request
			default:
				//unexpected error
				return err
			}
		}
	}

	_, err := Request{
		Method:            "DELETE",
		ContainerName:     o.c.name,
		ObjectName:        o.name,
		Options:           ropts,
		ExpectStatusCodes: []int{204},
	}.Do(o.c.a.backend)
	if err == nil {
		o.Invalidate()
	}
	return err
}

//Invalidate clears the internal cache of this Object instance. The next call
//to Headers() on this instance will issue a HEAD request on the object.
func (o *Object) Invalidate() {
	o.headers = nil
	o.symlinkHeaders = nil
}

//Download retrieves the object's contents using a GET request. This returns a
//helper object which allows you to select whether you want an io.ReadCloser
//for reading the object contents progressively, or whether you want the object
//contents collected into a byte slice or string.
//
//	reader, err := object.Download(nil).AsReadCloser()
//
//	buf, err := object.Download(nil).AsByteSlice()
//
//	str, err := object.Download(nil).AsString()
//
//See documentation on type DownloadedObject for details.
func (o *Object) Download(opts *RequestOptions) DownloadedObject {
	resp, err := Request{
		Method:            "GET",
		ContainerName:     o.c.name,
		ObjectName:        o.name,
		Options:           opts,
		ExpectStatusCodes: []int{200},
	}.Do(o.c.a.backend)
	var body io.ReadCloser
	if err == nil {
		newHeaders := ObjectHeaders{headersFromHTTP(resp.Header)}
		err = newHeaders.Validate()
		if err == nil {
			if opts != nil && opts.Values != nil && opts.Values.Get("symlink") == "get" {
				o.symlinkHeaders = &newHeaders
			} else {
				o.headers = &newHeaders
			}
		}
		body = resp.Body
	}
	return DownloadedObject{body, err}
}

//CopyOptions invokes advanced behavior in the Object.Copy() method.
type CopyOptions struct {
	//Copy only the object's content, not its metadata. New metadata can always
	//be supplied in the RequestOptions argument of Object.CopyTo().
	FreshMetadata bool
	//When the source is a symlink, copy the symlink instead of the target object.
	ShallowCopySymlinks bool
}

//CopyTo copies the object on the server side using a COPY request.
//
//A successful COPY implies target.Invalidate() since it may change the
//target's metadata.
func (o *Object) CopyTo(target *Object, opts *CopyOptions, ropts *RequestOptions) error {
	ropts = cloneRequestOptions(ropts, nil)
	ropts.Headers.Set("Destination", target.FullName())
	if o.c.a.name != target.c.a.name {
		ropts.Headers.Set("Destination-Account", target.c.a.name)
	}
	if opts != nil {
		if opts.FreshMetadata {
			ropts.Headers.Set("X-Fresh-Metadata", "true")
		}
		if opts.ShallowCopySymlinks {
			ropts.Values.Set("symlink", "get")
		}
	}

	_, err := Request{
		Method:            "COPY",
		ContainerName:     o.c.name,
		ObjectName:        o.name,
		Options:           ropts,
		ExpectStatusCodes: []int{201},
		DrainResponseBody: true,
	}.Do(o.c.a.backend)
	if err == nil {
		target.Invalidate()
	}
	return err
}

//SymlinkOptions invokes advanced behavior in the Object.SymlinkTo() method.
type SymlinkOptions struct {
	//When overwriting a large object, delete its segments. This will cause
	//SymlinkTo() to call into BulkDelete(), so a BulkError may be returned.
	DeleteSegments bool
}

//SymlinkTo creates the object as a symbolic link to another object using a PUT
//request. Like Object.Upload(), this method works regardless of whether the
//object already exists or not. Existing object contents will be overwritten by
//this operation.
//
//A successful PUT request implies Invalidate() since it may change metadata.
func (o *Object) SymlinkTo(target *Object, opts *SymlinkOptions, ropts *RequestOptions) error {
	ropts = cloneRequestOptions(ropts, nil)
	ropts.Headers.Set("X-Symlink-Target", target.FullName())
	if !target.c.a.IsEqualTo(o.c.a) {
		ropts.Headers.Set("X-Symlink-Target-Account", target.c.a.Name())
	}
	if ropts.Headers.Get("Content-Type") == "" {
		//recommended Content-Type for symlinks as per
		//<https://docs.openstack.org/swift/latest/middleware.html#symlink>
		ropts.Headers.Set("Content-Type", "application/symlink")
	}

	var uopts *UploadOptions
	if opts != nil {
		uopts = &UploadOptions{
			DeleteSegments: opts.DeleteSegments,
		}
	}

	return o.Upload(nil, uopts, ropts)
}

//SymlinkHeaders is similar to Headers, but if the object is a symlink, it
//returns the metadata of the symlink rather than the metadata of the target.
//It also returns a reference to the target object.
//
//If this object is not a symlink, Object.SymlinkHeaders() returns the same
//ObjectHeaders as Object.Headers(), and a nil target object.
//
//In a nutshell, if Object.Headers() is like os.Stat(), then
//Object.SymlinkHeaders() is like os.Lstat().
//
//If you do not know whether a given object is a symlink or not, it's a good
//idea to call Object.SymlinkHeaders() first: If the object turns out not to be
//a symlink, the cache for Object.Headers() has already been populated.
//
//This operation fails with http.StatusNotFound if the object does not exist.
func (o *Object) SymlinkHeaders() (headers ObjectHeaders, target *Object, err error) {
	if o.symlinkHeaders == nil {
		o.symlinkHeaders, err = o.fetchHeaders(&RequestOptions{
			Values: url.Values{"symlink": []string{"get"}},
		})
		if err != nil {
			return ObjectHeaders{}, nil, err
		}
	}

	//is this a symlink?
	targetFullName := o.symlinkHeaders.Get("X-Symlink-Target")
	if targetFullName == "" {
		//not a symlink - the o.symlinkHeaders are just the regular headers
		o.headers = o.symlinkHeaders
		return *o.headers, nil, nil
	}
	fields := strings.SplitN(targetFullName, "/", 2)
	if len(fields) < 2 {
		return ObjectHeaders{}, nil, MalformedHeaderError{
			Key:        "X-Symlink-Target",
			ParseError: fmt.Errorf("expected \"container/object\", got \"%s\"", targetFullName),
		}
	}

	//cross-account symlink?
	accountName := o.symlinkHeaders.Get("X-Symlink-Target-Account")
	targetAccount := o.c.a
	if accountName != "" && accountName != targetAccount.Name() {
		targetAccount = targetAccount.SwitchAccount(accountName)
	}
	target = targetAccount.Container(fields[0]).Object(fields[1])
	return *o.symlinkHeaders, target, nil
}

//URL returns the canonical URL for the object on the server. This is
//particularly useful when the ReadACL on the account or container is set to
//allow anonymous read access.
func (o *Object) URL() (string, error) {
	return Request{
		ContainerName: o.c.name,
		ObjectName:    o.name,
	}.URL(o.c.a.backend, nil)
}

//TempURL is like Object.URL, but includes a token with a limited lifetime (as
//specified by the `expires` argument) that permits anonymous access to this
//object using the given HTTP method. This works only when the tempurl
//middleware is set up on the server, and if the given `key` matches one of the
//tempurl keys for this object's container or account.
//
//For example, if the ReadACL both on the account and container do not permit
//anonymous read access (which is the default behavior):
//
//	var o *schwift.Object
//	...
//	resp, err := http.Get(o.URL())
//	//After this, resp.StatusCode == 401 (Unauthorized)
//	//because anonymous access is forbidden.
//
//	//But if the container or account has a tempurl key...
//	key := "supersecretkey"
//	hdr := NewContainerHeaders()
//	hdr.TempURLKey().Set(key)
//	c := o.Container()
//	err := c.Update(hdr, nil)
//
//	//...we can use it to generate temporary URLs.
//	url := o.TempURL(key, "GET", time.Now().Add(10 * time.Minute))
//	resp, err := http.Get(url)
//	//This time, resp.StatusCode == 200 because the URL includes a token.
//
func (o *Object) TempURL(key, method string, expires time.Time) (string, error) {
	urlStr, err := o.URL()
	if err != nil {
		return "", err
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}

	payload := fmt.Sprintf("%s\n%d\n%s", method, expires.Unix(), u.Path)
	mac := hmac.New(sha1.New, []byte(key))
	mac.Write([]byte(payload))
	signature := hex.EncodeToString(mac.Sum(nil))

	u.RawQuery = fmt.Sprintf("temp_url_sig=%s&temp_url_expires=%d",
		signature, expires.Unix())
	return u.String(), nil
}
