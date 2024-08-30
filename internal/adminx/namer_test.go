package adminx

import "testing"

func TestNamer_GetProjectsName(t *testing.T) {
	tests := []struct {
		name        string
		proj        string
		org         string
		wantProject string
		wantSAID    string
		wantSAEmail string
		wantSAName  string
		wantSecID   string
		wantSecName string
	}{
		{
			name:        "success",
			proj:        "mlab-sandbox",
			org:         "foo",
			wantProject: "projects/mlab-sandbox",
			wantSAID:    "autonode-gcsrw-foo",
			wantSAEmail: "autonode-gcsrw-foo@mlab-sandbox.iam.gserviceaccount.com",
			wantSAName:  "projects/mlab-sandbox/serviceAccounts/autonode-gcsrw-foo@mlab-sandbox.iam.gserviceaccount.com",
			wantSecID:   "autojoin-secret-foo",
			wantSecName: "projects/mlab-sandbox/secrets/autojoin-secret-foo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := NewNamer(tt.proj)
			if got := n.GetProjectsName(); got != tt.wantProject {
				t.Errorf("Namer.GetProjectsName() = %v, want %v", got, tt.wantProject)
			}
			if got := n.GetServiceAccountID(tt.org); got != tt.wantSAID {
				t.Errorf("Namer.GetServiceAccountID() = %v, want %v", got, tt.wantSAID)
			}
			if got := n.GetServiceAccountEmail(tt.org); got != tt.wantSAEmail {
				t.Errorf("Namer.GetServiceAccountEmail() = %v, want %v", got, tt.wantSAEmail)
			}
			if got := n.GetServiceAccountName(tt.org); got != tt.wantSAName {
				t.Errorf("Namer.GetServiceAccountName() = %v, want %v", got, tt.wantSAName)
			}
			if got := n.GetSecretID(tt.org); got != tt.wantSecID {
				t.Errorf("Namer.GetSecretID() = %v, want %v", got, tt.wantSecID)
			}
			if got := n.GetSecretName(tt.org); got != tt.wantSecName {
				t.Errorf("Namer.GetSecretName() = %v, want %v", got, tt.wantSecName)
			}
		})
	}
}
