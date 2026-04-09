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

package auth

import (
	"fmt"
	"os"
)

// WarnIfUnauthenticated prints a warning to stderr when Butler credentials
// are not being used. This alerts operators that they are bypassing Butler
// RBAC by using a raw kubeconfig.
func WarnIfUnauthenticated() {
	creds, err := LoadCredentials()
	if err != nil || creds.ActiveCredential() == nil {
		fmt.Fprintln(os.Stderr, "Warning: using raw kubeconfig without Butler authentication. Run 'butleradm login' to authenticate.")
	}
}
