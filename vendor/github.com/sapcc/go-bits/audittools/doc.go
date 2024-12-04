/*******************************************************************************
*
* Copyright 2019 SAP SE
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

/*
Package audittools provides a microframework for establishing a connection to
a RabbitMQ server (with sane defaults) and publishing audit messages in the CADF format to it.

To use it, build an AuditTrail object and spawn its Commit() event loop at initialization time.
Then push events into it as part of your request handlers.
Check the example on type AuditTrail for details.
*/
package audittools
