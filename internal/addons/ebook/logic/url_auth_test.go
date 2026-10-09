package album

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"
)

func TestOptionalURLAuthConfiguration(t *testing.T) {
	t.Chdir(t.TempDir())
	m := installMockPan(t)
	m.unsignedCDN = true
	off, on, english := false, true, int64(200)
	var legacy StorageProfile
	if err := json.Unmarshal([]byte(`{"id":"legacy"}`), &legacy); err != nil || !legacy.URLAuthEnabled() {
		t.Fatal("legacy configuration must require signatures")
	}
	p := StorageProfile{ClientID: "optional", ClientSecret: "secret", ParentID: 100, EnglishParentID: &english, URLAuth: &off}
	id, err := SaveStorageProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = CheckStorage(context.Background(), id); err != nil {
		t.Fatal("unsigned mode must work without a CDN key", err)
	}
	p, _ = storageProfile(id)
	if p.URLAuthEnabled() || p.CDNTTL() != 0 || p.VerifiedAt == 0 {
		t.Fatal("unsigned configuration not persisted")
	}
	if len(m.files) != 4 {
		t.Fatal("both directories must test images and PDFs")
	}
	// An old client omitting the new field must preserve a saved false value.
	p.URLAuth = nil
	if _, err = SaveStorageProfile(p); err != nil {
		t.Fatal(err)
	}
	p, _ = storageProfile(id)
	if p.URLAuthEnabled() || p.VerifiedAt == 0 {
		t.Fatal("omitted flag reset unsigned mode")
	}
	if err = ActivateStorage(id); err != nil {
		t.Fatal(err)
	}
	p.URLAuth = &on
	if _, err = SaveStorageProfile(p); err == nil {
		t.Fatal("active profile changed without deactivation")
	}
	if err = ActivateStorage("local"); err != nil {
		t.Fatal(err)
	}
	p.CDNKey = "test-key"
	before := p.Revision
	if _, err = SaveStorageProfile(p); err != nil {
		t.Fatal(err)
	}
	p, _ = storageProfile(id)
	if p.VerifiedAt != 0 || p.Revision <= before {
		t.Fatal("mode change must invalidate verification")
	}
	if err = ActivateStorage(id); err == nil {
		t.Fatal("unverified activation")
	}
	if err = CheckStorage(context.Background(), id); err == nil {
		t.Fatal("signed mode accepted a CDN that allows unsigned access")
	}
	p.URLAuth = &off
	if _, err = SaveStorageProfile(p); err != nil {
		t.Fatal(err)
	}
	m.unsignedCDN = false
	if err = CheckStorage(context.Background(), id); err == nil {
		t.Fatal("unsigned mode accepted a CDN requiring signatures")
	}
	current, _ := CurrentStorageTarget()
	if current.ID != "" {
		t.Fatal("failed probe changed active storage")
	}
}

func TestOptionalURLAuthAddresses(t *testing.T) {
	off := false
	p := StorageProfile{URLAuth: &off}
	raw := "https://13.cdn.123clouddisk.com/13/file.png"
	address, err := profileCDNURL(raw+"?auth_key=stale#fragment", p, time.Now().Add(time.Minute))
	if err != nil || address != raw {
		t.Fatal("unsigned URL", err)
	}
	for _, invalid := range []string{"http://13.cdn.123clouddisk.com/a", "https://127.0.0.1/a", "https://localhost/a", "https://user:pass@example.com/a"} {
		if _, err = profileCDNURL(invalid, p, time.Now()); err == nil {
			t.Fatal("unsafe unsigned address", invalid)
		}
	}
	p = StorageProfile{UID: 13, CDNKey: "test-key"}
	address, err = profileCDNURL(raw, p, time.Now().Add(time.Minute))
	u, _ := url.Parse(address)
	if err != nil || !validMockSignature(u, "test-key") || p.CDNTTL() != 60 {
		t.Fatal("default signed mode changed", err)
	}
}
