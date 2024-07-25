/*******************************************************************************
*
* Copyright 2024 SAP SE
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

package liquid

// OvercommitFactor is the ratio between raw and effective capacity of a resource.
// It appears in type ResourceDemand.
//
// In its methods, the zero value behaves as 1, meaning that no overcommit is taking place.
type OvercommitFactor float64

// ApplyTo converts a raw capacity into an effective capacity.
func (f OvercommitFactor) ApplyTo(rawCapacity uint64) uint64 {
	if f == 0 {
		// if no overcommit was configured, assume an overcommit factor of 1
		return rawCapacity
	}
	return uint64(float64(rawCapacity) * float64(f))
}

// ApplyInReverseTo turns the given effective capacity back into a raw capacity.
func (f OvercommitFactor) ApplyInReverseTo(capacity uint64) uint64 {
	if f == 0 {
		// if no overcommit was configured, assume an overcommit factor of 1
		return capacity
	}
	rawCapacity := uint64(float64(capacity) / float64(f))
	for f.ApplyTo(rawCapacity) < capacity {
		// fix errors from rounding down float64 -> uint64 above
		rawCapacity++
	}
	return rawCapacity
}

// ApplyInReverseToDemand is a shorthand for calling ApplyInReverseTo() on all fields of a ResourceDemand,
// thus turning all values initially given in terms of effective capacity into the corresponding raw capacity.
func (f OvercommitFactor) ApplyInReverseToDemand(demand ResourceDemandInAZ) ResourceDemandInAZ {
	return ResourceDemandInAZ{
		Usage:              f.ApplyInReverseTo(demand.Usage),
		UnusedCommitments:  f.ApplyInReverseTo(demand.UnusedCommitments),
		PendingCommitments: f.ApplyInReverseTo(demand.PendingCommitments),
	}
}
