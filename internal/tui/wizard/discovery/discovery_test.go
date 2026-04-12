/*
Copyright 2026 The Butler Authors.

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

package discovery

import (
	"context"
	"strings"
	"testing"
)

// mockCreds implements CredentialProvider for testing.
type mockCreds struct {
	values map[string]string
	bools  map[string]bool
}

func (m *mockCreds) Get(key string) string {
	if m.values == nil {
		return ""
	}
	return m.values[key]
}

func (m *mockCreds) GetBool(key string) bool {
	if m.bools == nil {
		return false
	}
	return m.bools[key]
}

// mockDiscovery is a trivial ProviderDiscovery for testing registration.
type mockDiscovery struct{}

func (m *mockDiscovery) Connect(ctx context.Context) error                    { return nil }
func (m *mockDiscovery) FetchResource(ctx context.Context, rt, pid string) ([]ProviderResource, error) {
	return nil, nil
}
func (m *mockDiscovery) ResourceTypes() []ResourceTypeInfo                    { return nil }
func (m *mockDiscovery) SyncImage(ctx context.Context, url, name string) (string, error) {
	return "", ErrNotSupported
}
func (m *mockDiscovery) PollImageSync(ctx context.Context, id string) (bool, string, error) {
	return false, "", ErrNotSupported
}

func TestNewDiscovery_Harvester(t *testing.T) {
	creds := &mockCreds{values: map[string]string{"kubeconfig": "/tmp/test"}}
	d, err := NewDiscovery("harvester", creds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil discovery")
	}
}

func TestNewDiscovery_Nutanix(t *testing.T) {
	creds := &mockCreds{values: map[string]string{
		"endpoint": "prism.example.com",
		"username": "admin",
		"password": "secret",
	}}
	d, err := NewDiscovery("nutanix", creds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil discovery")
	}
}

func TestNewDiscovery_UnknownProvider(t *testing.T) {
	creds := &mockCreds{}
	_, err := NewDiscovery("unknown-provider", creds)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("error should mention 'not available': %v", err)
	}
}

func TestRegisterAndNewDiscovery(t *testing.T) {
	Register("test-provider", func(creds CredentialProvider) (ProviderDiscovery, error) {
		return &mockDiscovery{}, nil
	})
	defer delete(registry, "test-provider")

	creds := &mockCreds{}
	d, err := NewDiscovery("test-provider", creds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil discovery")
	}
}
