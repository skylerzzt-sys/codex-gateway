package manager

import (
	"context"
	"encoding/json"

	"cpa-account-config-manager/internal/cpaapi"
)

func twoEditableAccountsHost() *fakeAuthHost {
	return &fakeAuthHost{
		entries: []cpaapi.HostAuthFileEntry{
			{AuthIndex: "a", Name: "a.json", Provider: "codex", Source: "file", Path: "/auths/a.json"},
			{AuthIndex: "b", Name: "b.json", Provider: "gemini", Source: "file", Path: "/auths/b.json"},
		},
		details: map[string]cpaapi.HostAuthGetResponse{
			"a": {AuthIndex: "a", Name: "a.json", Path: "/auths/a.json", JSON: json.RawMessage(`{"access_token":"secret-a"}`)},
			"b": {AuthIndex: "b", Name: "b.json", Path: "/auths/b.json", JSON: json.RawMessage(`{"access_token":"secret-b"}`)},
		},
	}
}

type panicWriter struct{}

func (panicWriter) PatchFields(context.Context, string, AccountPatch) error {
	panic("secret panic detail")
}

func (panicWriter) PatchDisabled(context.Context, string, bool) error {
	panic("secret panic detail")
}

func (panicWriter) DeleteAuthFile(context.Context, string) error {
	panic("secret panic detail")
}

type trackingWriter struct {
	key string
}

func (w *trackingWriter) PatchFields(context.Context, string, AccountPatch) error { return nil }
func (w *trackingWriter) PatchDisabled(context.Context, string, bool) error       { return nil }
func (w *trackingWriter) DeleteAuthFile(context.Context, string) error            { return nil }
func (w *trackingWriter) clearSecrets()                                           { w.key = "" }

func boolPointer(value bool) *bool { return &value }
