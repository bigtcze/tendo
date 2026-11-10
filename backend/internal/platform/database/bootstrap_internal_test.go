package database

import "testing"

// TestRuntimeRolePassword pins the credential contract the bootstrap accepts:
// only the fixed serving role, only with a password, and never by returning
// the password in an error.
func TestRuntimeRolePassword(t *testing.T) {
	for _, tc := range []struct {
		name    string
		url     string
		want    string
		wantErr string
	}{
		{name: "valid", url: "postgres://tendo:secret@db.internal:5432/tendo?sslmode=disable", want: "secret"},
		{name: "escaped password", url: "postgres://tendo:p%40ss%2Fword@db.internal:5432/tendo?sslmode=disable", want: "p@ss/word"},
		{
			name:    "other role",
			url:     "postgres://tendo_r_1:secret@db.internal:5432/tendo?sslmode=disable",
			wantErr: "DATABASE_URL must connect as role tendo when TENDO_DATABASE_ADMIN_URL is set",
		},
		{
			name:    "missing password",
			url:     "postgres://tendo@db.internal:5432/tendo?sslmode=disable",
			wantErr: "DATABASE_URL must include the role tendo password when TENDO_DATABASE_ADMIN_URL is set",
		},
		{
			name:    "invalid URL",
			url:     "::not-a-url",
			wantErr: "DATABASE_URL must connect as role tendo when TENDO_DATABASE_ADMIN_URL is set",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			password, err := runtimeRolePassword(tc.url)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("err=%v want %q", err, tc.wantErr)
				}
				if password != "" {
					t.Fatalf("password returned with error: %q", password)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if password != tc.want {
				t.Fatalf("password=%q want %q", password, tc.want)
			}
		})
	}
}
