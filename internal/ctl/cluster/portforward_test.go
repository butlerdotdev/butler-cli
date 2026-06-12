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

package cluster

import (
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// A tenant kubeconfig as Steward stores it: server is the docker-bridge LoadBalancer
// IP, which is not routable from a macOS/Windows host.
const tenantKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: toy
  cluster:
    server: https://172.18.255.201:6443
    certificate-authority-data: ` + fakeCA + `
contexts:
- name: toy
  context:
    cluster: toy
    user: toy-admin
current-context: toy
users:
- name: toy-admin
  user:
    client-certificate-data: ` + fakeCA + `
    client-key-data: ` + fakeCA + `
`

// fakeCA is a syntactically valid base64 blob; clientcmd does not validate the
// certificate contents on load/write.
const fakeCA = "dGVzdA=="

func TestPointClustersAtLocalPort(t *testing.T) {
	cfg, err := clientcmd.Load([]byte(tenantKubeconfig))
	if err != nil {
		t.Fatalf("loading fixture kubeconfig: %v", err)
	}

	pointClustersAtLocalPort(cfg, 52800)

	cl := cfg.Clusters["toy"]
	if cl == nil {
		t.Fatal("cluster entry 'toy' missing after rewrite")
	}
	if got, want := cl.Server, "https://127.0.0.1:52800"; got != want {
		t.Errorf("server = %q, want %q", got, want)
	}
	// The original host must be preserved as tls-server-name so the server cert
	// (SANs include 172.18.255.201) still verifies through the 127.0.0.1 tunnel.
	if got, want := cl.TLSServerName, "172.18.255.201"; got != want {
		t.Errorf("tls-server-name = %q, want %q", got, want)
	}
	// CA and user credentials must be left intact.
	if len(cl.CertificateAuthorityData) == 0 {
		t.Error("certificate-authority-data was dropped")
	}
	if u := cfg.AuthInfos["toy-admin"]; u == nil || len(u.ClientCertificateData) == 0 {
		t.Error("client credentials were dropped")
	}
}

func TestPointClustersAtLocalPort_EmptyServer(t *testing.T) {
	// A cluster entry with an empty server must not panic and should still be
	// pointed at the local port (tls-server-name simply stays empty).
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters["c"] = &clientcmdapi.Cluster{Server: ""}

	pointClustersAtLocalPort(cfg, 40000)

	if got, want := cfg.Clusters["c"].Server, "https://127.0.0.1:40000"; got != want {
		t.Errorf("server = %q, want %q", got, want)
	}
	if cfg.Clusters["c"].TLSServerName != "" {
		t.Errorf("tls-server-name = %q, want empty", cfg.Clusters["c"].TLSServerName)
	}
}
