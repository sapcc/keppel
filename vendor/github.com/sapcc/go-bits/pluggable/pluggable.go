/******************************************************************************
*
*  Copyright 2022 SAP SE
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

// Package pluggable is a tiny plugin factory library. A factory object can be
// set up that contains constructors for multiple different plugin types
// implementing a common interface. Then plugin objects can be instantiated by
// their plugin type identifier:
//
//	// The application must define a common interface for plugins that inherits
//	// from the `pluggable.Plugin` interface.
//	type MyPlugin interface {
//		pluggable.Plugin
//		ReadTheThing() (string, error)
//		WriteTheThing(string) error
//	}
//	var MyRegistry pluggable.Registry[MyPlugin]
//
//	// Plugins the implement the application's plugin interface can register
//	// themselves with the factory.
//	func init() {
//		MyRegistry.Add(func() MyPlugin { return MyImplementation{} })
//	}
//
//	// Plugin instances can be created by referring to the plugin type ID:
//	myInstance := MyRegistry.Instantiate("foobar")
//	if myInstance == nil {
//		panic("no foobar plugin!")
//	}
package pluggable

import "fmt"

// Plugin is the base interface for plugins that type Registry can instantiate.
type Plugin interface {
	// PluginTypeID must always return a constant string that is always the same
	// for all instances of one type. Registry uses this ID to identify the
	// plugin type that one particular constructor constructs.
	PluginTypeID() string
}

// Registry is a container holding factories for multiple different plugin
// types implementing a common interface. Refer to the package-level
// documentation for details.
type Registry[T Plugin] struct {
	factories map[string]func() T
}

// Add adds a new plugin type to this Registry. The factory function will be
// called once immediately to determine the PluginTypeID of the constructed
// type, then stored for when this plugin type is called for during
// Instantiate().
func (r *Registry[T]) Add(factory func() T) {
	if factory == nil {
		panic("cannot register plugin with factory = nil")
	}

	pluginTypeID := factory().PluginTypeID()
	if pluginTypeID == "" {
		panic(`cannot register plugin with pluginTypeID = ""`)
	}
	if _, exists := r.factories[pluginTypeID]; exists {
		panic(fmt.Sprintf("cannot register multiple plugins with pluginTypeID = %q", pluginTypeID))
	}

	if r.factories == nil {
		r.factories = make(map[string]func() T)
	}
	r.factories[pluginTypeID] = factory
}

// Instantiate returns a new instance of the given plugin type.
//
// If the requested plugin type is not known, T's zero value will be returned.
// Since T is usually an application-specific interface type, this means that
// nil will be returned.
func (r *Registry[T]) Instantiate(pluginTypeID string) T {
	factory, exists := r.factories[pluginTypeID]
	if exists {
		return factory()
	}
	var zero T
	return zero
}
