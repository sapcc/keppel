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

//Package capabilities contains feature switches that Schwift's unit tests can
//set to exercise certain fallback code paths in Schwift that they could not
//trigger otherwise.
//
//THIS IS A PRIVATE MODULE. It is not covered by any forwards or backwards
//compatiblity and may be gone at a moment's notice.
package capabilities

//AllowBulkDelete can be set to false to force Schwift to act as if the server
//does not support bulk deletion.
var AllowBulkDelete = true
