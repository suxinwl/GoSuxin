package suxinvideo

import "testing"

func TestMemberSMTPReadiness(t *testing.T) {
	for _, tc := range []struct {
		name, host, user, password, port string
		ready                            bool
		wantPort                         int
	}{
		{"missing-host", "", "sender@example.invalid", "fixture", "465", false, 465},
		{"missing-user", "smtp.example.invalid", " ", "fixture", "465", false, 465},
		{"missing-password", "smtp.example.invalid", "sender@example.invalid", "\t ", "465", false, 465},
		{"complete-default-port", " smtp.example.invalid ", " sender@example.invalid ", "fixture", "", true, 465},
		{"complete-configured-port", "smtp.example.invalid", "sender@example.invalid", "fixture", " 587 ", true, 587},
		{"invalid-port-text", "smtp.example.invalid", "sender@example.invalid", "fixture", "smtp", false, 465},
		{"invalid-port-zero", "smtp.example.invalid", "sender@example.invalid", "fixture", "0", false, 465},
		{"invalid-port-negative", "smtp.example.invalid", "sender@example.invalid", "fixture", "-1", false, 465},
		{"invalid-port-overflow", "smtp.example.invalid", "sender@example.invalid", "fixture", "65536", false, 465},
		{"port-boundary-low", "smtp.example.invalid", "sender@example.invalid", "fixture", "1", true, 1},
		{"port-boundary-high", "smtp.example.invalid", "sender@example.invalid", "fixture", "65535", true, 65535},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, ready := parseMemberSMTP(tc.host, tc.user, tc.password, tc.port)
			if ready != tc.ready || config.Port != tc.wantPort {
				t.Fatalf("readiness=%v port=%d", ready, config.Port)
			}
		})
	}
	config, ready := parseMemberSMTP(" smtp.example.invalid ", " sender@example.invalid ", " fixture ", "465")
	if !ready || config.Host != "smtp.example.invalid" || config.User != "sender@example.invalid" || config.Password != " fixture " {
		t.Fatal("host/user trim or SMTP password preservation failed")
	}
}
