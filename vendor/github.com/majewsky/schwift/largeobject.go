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
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jpillora/longestcommon"
)

//SegmentInfo describes a segment of a large object.
//
//For .RangeLength == 0, the segment consists of all the bytes in the backing
//object, after skipping the first .RangeOffset bytes. The default
//(.RangeOffset == 0) is to include the entire contents of the backing object.
//
//For .RangeLength > 0, the segment consists of that many bytes from the
//backing object, again after skipping the first .RangeOffset bytes.
//
//However, for .RangeOffset < 0, the segment consists of .RangeLength many bytes
//from the *end* of the backing object. (The concrete value for .RangeOffset is
//disregarded.) .RangeLength must be non-zero in this case.
//
//Sorry that specifying a range is that involved. I was just following orders ^W
//RFC 7233, section 3.1 here.
type SegmentInfo struct {
	Object      *Object
	SizeBytes   uint64
	Etag        string
	RangeLength uint64
	RangeOffset int64
	//Static Large Objects support data segments that are not backed by actual
	//objects. For those kinds of segments, only the Data attribute is set and
	//all other attributes are set to their default values (esp. .Object == nil).
	//
	//Data segments can only be used for small chunks of data because the SLO
	//manifest (the list of all SegmentInfo encoded as JSON) is severely limited
	//in size (usually to 8 MiB).
	Data []byte
}

type sloSegmentInfo struct {
	Path       string `json:"path,omitempty"`
	SizeBytes  uint64 `json:"size_bytes,omitempty"`
	Etag       string `json:"etag,omitempty"`
	Range      string `json:"range,omitempty"`
	DataBase64 string `json:"data,omitempty"`
}

//LargeObjectStrategy enumerates segmenting strategies supported by Swift.
type LargeObjectStrategy int

//A value of 0 for LargeObjectStrategy will instruct Schwift to choose a
//strategy itself. Right now, Schwift always chooses StaticLargeObject, but
//this behavior may change in future versions of Schwift, esp. if new
//strategies become available. The choice may also start to depend on the
//capabilities advertised by the server.
const (
	//StaticLargeObject is the default LargeObjectStrategy used by Schwift.
	StaticLargeObject LargeObjectStrategy = iota + 1
	//DynamicLargeObject is an older LargeObjectStrategy that is not recommended
	//for new applications because of eventual consistency problems and missing
	//support for several newer features (e.g. data segments, range specifications).
	DynamicLargeObject
)

//SegmentingOptions describes how an object is segmented. It is passed to
//Object.AsNewLargeObject().
//
//If Strategy is not set, a reasonable strategy is chosen; see documentation on
//LargeObjectStrategy for details.
//
//SegmentContainer must not be nil. A value of nil will cause Schwift to panic.
//If the SegmentContainer is not in the same account as the large object,
//ErrAccountMismatch will be returned by Schwift.
//
//If SegmentPrefix is empty, a reasonable default will be computed by
//Object.AsNewLargeObject(), using the format
//"<object-name>/<strategy>/<timestamp>", where strategy is either "slo" or
//"dlo".
type SegmentingOptions struct {
	Strategy         LargeObjectStrategy
	SegmentContainer *Container
	SegmentPrefix    string
}

////////////////////////////////////////////////////////////////////////////////

//LargeObject is a wrapper for type Object that performs operations specific to
//large objects, i.e. those objects which are uploaded in segments rather than
//all at once. It can be constructed with the Object.AsLargeObject() and
//Object.AsNewLargeObject() methods.
//
//The following example shows how to upload a large file from the filesystem to
//Swift (error handling elided for brevity):
//
//	file, err := os.Open(sourcePath)
//	segmentContainer, err := account.Container("segments").EnsureExists()
//
//	lo, err := o.AsNewLargeObject(schwift.SegmentingOptions {
//	    SegmentContainer: segmentContainer,
//	    //use defaults for everything else
//	}, &schwift.TruncateOptions {
//	    //if there's already a large object here, clean it up
//	    DeleteSegments: true,
//	})
//
//	err = lo.Append(contents, 1<<30) // 1<30 bytes = 1 GiB per segment
//	err = lo.WriteManifest(nil)
//
//Append() has a more low-level counterpart, AddSegment(). Both methods can be
//freely intermixed. AddSegment() is useful when you want to control the
//segments' metadata or use advanced features like range segments or data
//segments; see documentation over there.
//
//Writing to a large object must always be concluded by a call to
//WriteManifest() to link the new segments to the large object on the server
//side.
type LargeObject struct {
	object           *Object
	segmentContainer *Container
	segmentPrefix    string
	strategy         LargeObjectStrategy
	segments         []SegmentInfo
}

//Object returns the location of this large object (where its manifest is stored).
func (lo *LargeObject) Object() *Object {
	return lo.object
}

//SegmentContainer returns the container in which this object's segments are
//stored. For static large objects, some segments may also be located in
//different containers.
func (lo *LargeObject) SegmentContainer() *Container {
	return lo.segmentContainer
}

//SegmentPrefix returns the prefix shared by the names of all segments of this
//object. For static large objects, some segments may not be located in this
//prefix.
func (lo *LargeObject) SegmentPrefix() string {
	return lo.segmentPrefix
}

//Strategy returns the LargeObjectStrategy used by this object.
func (lo *LargeObject) Strategy() LargeObjectStrategy {
	return lo.strategy
}

//Segments returns a list of all segments for this object, in order.
func (lo *LargeObject) Segments() ([]SegmentInfo, error) {
	//NOTE: This method has an error return value because we might later switch
	//to loading segments lazily inside this method.
	return lo.segments, nil
}

//SegmentObjects returns a list of all segment objects referenced by this large
//object. Note that, in general,
//
//	len(lo.SegmentObjects()) <= len(lo.Segments())
//
//since one object may be backing multiple segments, and data segments are not
//backed by any object at all. No guarantee is made about the order in which
//objects appear in this list.
func (lo *LargeObject) SegmentObjects() []*Object {
	seen := make(map[string]bool)
	result := make([]*Object, 0, len(lo.segments))
	for _, segment := range lo.segments {
		if segment.Object == nil { //can happen because of data segments
			continue
		}
		fullName := segment.Object.FullName()
		if !seen[fullName] {
			result = append(result, segment.Object)
		}
		seen[fullName] = true
	}
	return result
}

//AsLargeObject opens an existing large object. If the given object does not
//exist, or if it is not a large object, ErrNotLarge will be returned. In this
//case, Object.AsNewLargeObject() needs to be used instead.
func (o *Object) AsLargeObject() (*LargeObject, error) {
	exists, err := o.Exists()
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotLarge
	}

	h := o.headers
	if h.IsDynamicLargeObject() {
		return o.asDLO(h.Get("X-Object-Manifest"))
	}
	if h.IsStaticLargeObject() {
		return o.asSLO()
	}
	return nil, ErrNotLarge
}

func (o *Object) asDLO(manifestStr string) (*LargeObject, error) {
	manifest := strings.SplitN(manifestStr, "/", 2)
	if len(manifest) < 2 {
		return nil, ErrNotLarge
	}

	lo := &LargeObject{
		object:           o,
		segmentContainer: o.c.a.Container(manifest[0]),
		segmentPrefix:    manifest[1],
		strategy:         DynamicLargeObject,
	}

	iter := lo.segmentContainer.Objects()
	iter.Prefix = lo.segmentPrefix
	segmentInfos, err := iter.CollectDetailed()
	if err != nil {
		return nil, err
	}
	lo.segments = make([]SegmentInfo, 0, len(segmentInfos))
	for _, info := range segmentInfos {
		lo.segments = append(lo.segments, SegmentInfo{
			Object:    info.Object,
			SizeBytes: info.SizeBytes,
			Etag:      info.Etag,
		})
	}

	return lo, nil
}

func (o *Object) asSLO() (*LargeObject, error) {
	opts := RequestOptions{
		Values: make(url.Values),
	}
	opts.Values.Set("multipart-manifest", "get")
	opts.Values.Set("format", "raw")
	buf, err := o.Download(&opts).AsByteSlice()
	if err != nil {
		return nil, err
	}

	var data []sloSegmentInfo
	err = json.Unmarshal(buf, &data)
	if err != nil {
		return nil, errors.New("invalid SLO manifest: " + err.Error())
	}

	lo := &LargeObject{
		object:   o,
		strategy: StaticLargeObject,
	}
	if len(data) == 0 {
		return lo, nil
	}

	//read the segments first, then deduce the SegmentContainer/SegmentPrefix from these
	lo.segments = make([]SegmentInfo, 0, len(data))
	for _, info := range data {
		//option 1: data segment
		if info.DataBase64 != "" {
			data, err := base64.StdEncoding.DecodeString(info.DataBase64)
			if err != nil {
				return nil, errors.New("invalid SLO data segment: " + err.Error())
			}
			lo.segments = append(lo.segments, SegmentInfo{Data: data})
			continue
		}

		//option 2: segment backed by object
		pathElements := strings.SplitN(strings.TrimPrefix(info.Path, "/"), "/", 2)
		if len(pathElements) != 2 {
			return nil, errors.New("invalid SLO segment: malformed path: " + info.Path)
		}
		s := SegmentInfo{
			Object:    o.c.a.Container(pathElements[0]).Object(pathElements[1]),
			SizeBytes: info.SizeBytes,
			Etag:      info.Etag,
		}
		if info.Range != "" {
			var ok bool
			s.RangeOffset, s.RangeLength, ok = parseHTTPRange(info.Range)
			if !ok {
				return nil, errors.New("invalid SLO segment: malformed range: " + info.Range)
			}
		}
		lo.segments = append(lo.segments, s)
	}

	//choose the SegmentContainer by majority vote (in the spirit of "be liberal
	//in what you accept")
	containerNames := make(map[string]uint)
	for _, s := range lo.segments {
		if s.Object == nil { //can happen for data segments
			continue
		}
		containerNames[s.Object.c.Name()]++
	}
	maxName := ""
	maxVotes := uint(0)
	for name, votes := range containerNames {
		if votes > maxVotes {
			maxName = name
			maxVotes = votes
		}
	}
	lo.segmentContainer = lo.object.c.a.Container(maxName)

	//choose the SegmentPrefix as the longest common prefix of all segments in
	//the chosen SegmentContainer...
	names := make([]string, 0, len(lo.segments))
	for _, s := range lo.segments {
		if s.Object == nil { //can happen for data segments
			continue
		}
		name := s.Object.c.Name()
		if name == maxName {
			names = append(names, s.Object.Name())
		}
	}
	lo.segmentPrefix = longestcommon.Prefix(names)

	//..BUT if the prefix is a path with slashes, do not consider the part after
	//the last slash; e.g. if we have segments "foo/bar/0001" and "foo/bar/0002",
	//the longest common prefix is "foo/bar/000", but we actually want "foo/bar/"
	if strings.Contains(lo.segmentPrefix, "/") {
		lo.segmentPrefix = path.Dir(lo.segmentPrefix) + "/"
	}

	return lo, nil
}

func parseHTTPRange(str string) (offsetVal int64, lengthVal uint64, ok bool) {
	fields := strings.SplitN(str, "-", 2)
	if len(fields) != 2 {
		return 0, 0, false
	}

	if fields[0] == "" {
		//case 1: "-"
		if fields[1] == "" {
			return 0, 0, true
		}

		//case 2: "-N"
		numBytes, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, 0, false
		}
		return -1, numBytes, true
	}

	firstByte, err := strconv.ParseUint(fields[0], 10, 63) //not 64; needs to be unsigned, but also fit into int64
	if err != nil {
		return 0, 0, false
	}
	if fields[1] == "" {
		//case 3: "N-"
		return int64(firstByte), 0, true
	}
	//case 4: "M-N"
	lastByte, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil || lastByte < firstByte {
		return 0, 0, false
	}
	return int64(firstByte), lastByte - firstByte + 1, true
}

//AsNewLargeObject opens an object as a large object. SegmentingOptions are
//always required, see the documentation on type SegmentingOptions for details.
//
//This function can be used regardless of whether the object exists or not.
//If the object exists and is a large object, this function behaves like
//Object.AsLargeObject() followed by Truncate(), except that segmenting options
//are initialized from the method's SegmentingOptions argument rather than from
//the existing manifest.
func (o *Object) AsNewLargeObject(sopts SegmentingOptions, topts *TruncateOptions) (*LargeObject, error) {
	//we only need to load the existing large object if we want to do something
	//with the old segments
	if topts != nil && topts.DeleteSegments {
		lo, err := o.AsLargeObject()
		switch err {
		case nil:
			err := lo.Truncate(topts)
			if err != nil {
				return nil, err
			}
		case ErrNotLarge:
			//not an error, continue down below
			err = nil
		default:
			return nil, err //unexpected error
		}
	}

	lo := &LargeObject{object: o}

	//validate segment container
	lo.segmentContainer = sopts.SegmentContainer
	if sopts.SegmentContainer == nil {
		panic("missing value for sopts.SegmentingContainer")
	}
	if !sopts.SegmentContainer.a.IsEqualTo(o.c.a) {
		return nil, ErrAccountMismatch
	}

	//apply default value for strategy
	if sopts.Strategy == 0 {
		lo.strategy = StaticLargeObject
	} else {
		lo.strategy = sopts.Strategy
	}

	//apply default value for segmenting prefix
	lo.segmentPrefix = sopts.SegmentPrefix
	if lo.segmentPrefix == "" {
		now := time.Now()
		strategyStr := "slo"
		if lo.strategy == DynamicLargeObject {
			strategyStr = "dlo"
		}

		lo.segmentPrefix = fmt.Sprintf("%s/%s/%d.%09d",
			o.Name(), strategyStr, now.Unix(), now.Nanosecond(),
		)
	}

	return lo, nil
}

//TruncateOptions contains options that can be passed to LargeObject.Truncate()
//and Object.AsNewLargeObject().
type TruncateOptions struct {
	//When truncating a large object's manifest, delete its segments.
	//This will cause Truncate() to call into BulkDelete(), so a BulkError may be
	//returned. If this is false, the segments will not be deleted even though
	//they may not be referenced by any large object anymore.
	DeleteSegments bool
}

//Truncate removes all segments from a large object's manifest. The manifest is
//not written by this call, so WriteManifest() usually needs to be called
//afterwards.
func (lo *LargeObject) Truncate(opts *TruncateOptions) error {
	_, _, err := lo.object.c.a.BulkDelete(lo.SegmentObjects(), nil, nil)
	if err == nil {
		lo.segments = nil
	}
	return err
}

//NextSegmentObject suggests where to upload the next segment.
//
//WARNING: This is a low-level function. Most callers will want to use
//Append(). You will only need to upload segments manually when you want to
//control the segments' metadata.
//
//If the name of the current final segment ends with a counter, that counter is
//incremented, otherwise a counter is appended to its name. When looking for a
//counter in an existing segment name, the regex /[0-9]+$/ is used. For example,
//given:
//
//	segments := lo.segments()
//	lastSegmentName := segments[len(segments)-1].Name()
//	nextSegmentName := lo.NextSegmentObject().Name()
//
//If lastSegmentName is "segments/archive/segment0001", then nextSegmentName is
//"segments/archive/segment0002". If lastSegmentName is
//"segments/archive/first", then nextSegmentName is
//"segments/archive/first0000000000000001".
//
//However, the last segment's name will only be considered if it lies within
//lo.segmentContainer below lo.segmentPrefix. If that is not the case, the name
//of the last segment that does will be used instead.
//
//If there are no segments yet, or if all segments are located outside the
//lo.segmentContainer and lo.segmentPrefix, the first segment name is chosen as
//lo.segmentPrefix + "0000000000000001".
func (lo *LargeObject) NextSegmentObject() *Object {
	//find the name of the last-most segment that is within the designated
	//segment container and prefix
	var prevSegmentName string
	for _, s := range lo.segments {
		o := s.Object
		if o == nil { //can happen for data segments
			continue
		}
		if lo.segmentContainer.IsEqualTo(o.c) && strings.HasPrefix(o.Name(), lo.segmentPrefix) {
			prevSegmentName = s.Object.Name()
			//keep going, we want to find the last such segment
		}
	}

	//choose the next segment name based on the previous one
	var segmentName string
	if prevSegmentName == "" {
		segmentName = lo.segmentPrefix + initialIndex
	} else {
		segmentName = nextSegmentName(prevSegmentName)
	}

	return lo.segmentContainer.Object(segmentName)
}

var splitSegmentIndexRx = regexp.MustCompile(`^(.*?)([0-9]+$)`)
var initialIndex = "0000000000000001"

//Given the object name of a previous large object segment, compute a suitable
//name for the next segment. See doc for LargeObject.NextSegmentObject()
//for how this works.
func nextSegmentName(segmentName string) string {
	match := splitSegmentIndexRx.FindStringSubmatch(segmentName)
	if match == nil {
		return segmentName + initialIndex
	}
	base, idxStr := match[1], match[2]

	idx, err := strconv.ParseUint(idxStr, 10, 64)
	if err != nil || idx == math.MaxUint64 { //overflow
		//start from one again, but separate with a dash to ensure that the new
		//index can be parsed properly in the next call to this function
		return segmentName + "-" + initialIndex
	}

	//print next index with same number of digits as previous index,
	//e.g. "00001" -> "00002" (except if overflow, e.g. "9999" -> "10000")
	formatStr := fmt.Sprintf("%%0%dd", len(idxStr))
	return base + fmt.Sprintf(formatStr, idx+1)
}

//AddSegment appends a segment to this object. The segment must already have
//been uploaded.
//
//WARNING: This is a low-level function. Most callers will want to use
//Append(). You will only need to add segments manually when you want to
//control the segments' metadata, or when using advanced features such as
//range-limited segments or data segments.
//
//This method returns ErrAccountMismatch if the segment is not located in a
//container in the same account.
//
//For dynamic large objects, this method returns ErrContainerMismatch if the
//segment is not located in the correct container below the correct prefix.
//
//This method returns ErrSegmentInvalid if:
//
//- a range is specified in the SegmentInfo, but it is invalid or the
//LargeObject is a dynamic large object (DLOs do not support ranges), or
//
//- the SegmentInfo's Data attribute is set and any other attribute is also
//set (segments cannot be backed by objects and be data segments at the same
//time), or
//
//- the SegmentInfo's Data attribute is set, but the LargeObject is a dynamic
//large objects (DLOs do not support data segments).
func (lo *LargeObject) AddSegment(segment SegmentInfo) error {
	if len(segment.Data) == 0 {
		//validate segments backed by objects
		o := segment.Object
		if o == nil {
			//required attributes
			return ErrSegmentInvalid
		}
		if !o.c.a.IsEqualTo(lo.segmentContainer.a) {
			return ErrAccountMismatch
		}

		switch lo.strategy {
		case DynamicLargeObject:
			if segment.RangeLength != 0 || segment.RangeOffset != 0 {
				//not supported for DLO
				return ErrSegmentInvalid
			}

			if !o.c.IsEqualTo(lo.segmentContainer) {
				return ErrContainerMismatch
			}
			if !strings.HasPrefix(o.name, lo.segmentPrefix) {
				return ErrContainerMismatch
			}

		case StaticLargeObject:
			if segment.RangeLength == 0 && segment.RangeOffset < 0 {
				//malformed range
				return ErrSegmentInvalid
			}
		}
	} else {
		//validate plain-data segments
		if lo.strategy != StaticLargeObject {
			//not supported for DLO
			return ErrSegmentInvalid
		}
		if segment.Object != nil || segment.SizeBytes != 0 || segment.Etag != "" || segment.RangeLength != 0 || segment.RangeOffset != 0 {
			//all other attributes must be unset
			return ErrSegmentInvalid
		}
	}

	lo.segments = append(lo.segments, segment)
	return nil
}

//Append uploads the contents of the given io.Reader as segment objects of the
//given segment size. (The last segment will be shorter than the segment size
//unless the reader yields an exact multiple of the segment size.) The reader
//is consumed until EOF, or until an error occurs.
//
//If you do not have an io.Reader, but you have a []byte or string instance
//containing the data, wrap it in a *bytes.Reader instance like so:
//
//	var buffer []byte
//	lo.Append(bytes.NewReader(buffer), segmentSizeBytes)
//
//	//or...
//	var buffer string
//	lo.Append(bytes.NewReader([]byte(buffer)), segmentSizeBytes)
//
//If segmentSizeBytes is zero, Append() defaults to the maximum file size
//reported by Account.Capabilities().
//
//Calls to Append() and its low-level counterpart, AddSegment(), can be freely
//intermixed. AddSegment() is useful when you want to control the segments'
//metadata or use advanced features like range segments or data segments; see
//documentation over there.
//
//This function uploads segment objects, so it may return any error that
//Object.Upload() returns, see documentation over there.
func (lo *LargeObject) Append(contents io.Reader, segmentSizeBytes int64, opts *RequestOptions) error {
	if segmentSizeBytes < 0 {
		panic("segmentSizeBytes may not be negative")
	}
	if segmentSizeBytes == 0 {
		//apply default value for segmenting size
		caps, err := lo.object.c.a.Capabilities()
		if err != nil {
			return err
		}
		segmentSizeBytes = int64(caps.Swift.MaximumFileSize)
		if segmentSizeBytes <= 0 {
			return errors.New("cannot infer SegmentSizeBytes from Swift /info")
		}
	}

	sr := segmentingReader{contents, segmentSizeBytes}
	for {
		segment := sr.NextSegment()
		if segment == nil {
			break
		}

		tracker := lengthAndEtagTrackingReader{
			Reader: segment,
			Hasher: md5.New(),
		}

		obj := lo.NextSegmentObject()
		err := obj.Upload(&tracker, nil, opts)
		if err != nil {
			return err
		}
		err = lo.AddSegment(SegmentInfo{
			Object:    obj,
			SizeBytes: tracker.BytesRead,
			Etag:      hex.EncodeToString(tracker.Hasher.Sum(nil)),
		})
		if err != nil {
			return err
		}
	}

	return nil
}

type segmentingReader struct {
	Reader           io.Reader
	SegmentSizeBytes int64 //must be >0
}

func (sr *segmentingReader) NextSegment() io.Reader {
	//peek if there is more content in the backing reader
	buf := make([]byte, 1)
	var (
		n   int
		err error
	)
	for n == 0 {
		n, err = sr.Reader.Read(buf)
		if err == io.EOF {
			if n == 0 {
				//EOF encountered
				return nil
			}
			//that was the last byte - return only that (next NextSegment() will return nil)
			return bytes.NewReader(buf)
		}
	}

	//looks like there is more stuff in the backing reader
	return io.MultiReader(
		bytes.NewReader(buf),
		io.LimitReader(sr.Reader, sr.SegmentSizeBytes-1), //1 == len(buf)
	)
}

type lengthAndEtagTrackingReader struct {
	Reader    io.Reader
	BytesRead uint64
	Hasher    hash.Hash
}

func (r *lengthAndEtagTrackingReader) Read(buf []byte) (int, error) {
	n, err := r.Reader.Read(buf)
	r.BytesRead += uint64(n)
	r.Hasher.Write(buf[:n])
	return n, err
}

//WriteManifest creates this large object by writing a manifest to its
//location using a PUT request.
//
//For dynamic large objects, this method does not generate a PUT request
//if the object already exists and has the correct manifest (i.e.
//SegmentContainer and SegmentPrefix have not been changed).
func (lo *LargeObject) WriteManifest(opts *RequestOptions) error {
	switch lo.strategy {
	case StaticLargeObject:
		return lo.writeSLOManifest(opts)
	case DynamicLargeObject:
		return lo.writeDLOManifest(opts)
	default:
		panic("no such strategy")
	}
}

func (lo *LargeObject) writeDLOManifest(opts *RequestOptions) error {
	manifest := lo.segmentContainer.Name() + "/" + lo.segmentPrefix

	//check if the manifest is already set correctly
	headers, err := lo.object.Headers()
	if err != nil && !Is(err, http.StatusNotFound) {
		return err
	}
	if headers.Get("X-Object-Manifest") == manifest {
		return nil
	}

	//write manifest; make sure that this is a DLO
	opts = cloneRequestOptions(opts, nil)
	opts.Headers.Set("X-Object-Manifest", manifest)
	return lo.object.Upload(nil, nil, opts)
}

func (lo *LargeObject) writeSLOManifest(opts *RequestOptions) error {
	sloSegments := make([]sloSegmentInfo, len(lo.segments))
	for idx, s := range lo.segments {
		if len(s.Data) > 0 {
			sloSegments[idx] = sloSegmentInfo{
				DataBase64: base64.StdEncoding.EncodeToString(s.Data),
			}
		} else {
			si := sloSegmentInfo{
				Path:      "/" + s.Object.FullName(),
				SizeBytes: s.SizeBytes,
				Etag:      s.Etag,
			}

			if s.RangeOffset < 0 {
				si.Range = "-" + strconv.FormatUint(s.RangeLength, 10)
			} else {
				firstByteStr := strconv.FormatUint(uint64(s.RangeOffset), 10)
				lastByteStr := strconv.FormatUint(uint64(s.RangeOffset)+s.RangeLength-1, 10)
				si.Range = firstByteStr + "-" + lastByteStr
			}

			sloSegments[idx] = si
		}
	}

	manifest, err := json.Marshal(sloSegments)
	if err != nil {
		//failing json.Marshal() on such a trivial data structure is alarming
		panic(err.Error())
	}

	opts = cloneRequestOptions(opts, nil)
	opts.Headers.Del("X-Object-Manifest") //ensure sanity :)
	opts.Values.Set("multipart-manifest", "put")
	return lo.object.Upload(bytes.NewReader(manifest), nil, opts)
}
