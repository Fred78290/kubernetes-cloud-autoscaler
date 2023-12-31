/*
Copyright 2023 Frédéric Boltz.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis/nodemanager/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// ManagedNodeLister helps list ManagedNodes.
// All objects returned here must be treated as read-only.
type ManagedNodeLister interface {
	// List lists all ManagedNodes in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.ManagedNode, err error)
	// Get retrieves the ManagedNode from the index for a given name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.ManagedNode, error)
	ManagedNodeListerExpansion
}

// managedNodeLister implements the ManagedNodeLister interface.
type managedNodeLister struct {
	indexer cache.Indexer
}

// NewManagedNodeLister returns a new ManagedNodeLister.
func NewManagedNodeLister(indexer cache.Indexer) ManagedNodeLister {
	return &managedNodeLister{indexer: indexer}
}

// List lists all ManagedNodes in the indexer.
func (s *managedNodeLister) List(selector labels.Selector) (ret []*v1alpha1.ManagedNode, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.ManagedNode))
	})
	return ret, err
}

// Get retrieves the ManagedNode from the index for a given name.
func (s *managedNodeLister) Get(name string) (*v1alpha1.ManagedNode, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("managednode"), name)
	}
	return obj.(*v1alpha1.ManagedNode), nil
}