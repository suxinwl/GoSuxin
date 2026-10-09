package suxinvideo

import "context"

func builtInTheme(code string) bool {
	switch code {
	case "suxinlite", "suxinpro", "iqiyi", "guoguo":
		return true
	}
	return false
}

// Register the built-in theme on upgrades without replacing administrator
// settings or an existing theme record. Repeated startup is harmless.
func prepareGuoguoTheme(ctx context.Context) error {
	return execSQL(ctx, "INSERT IGNORE INTO sx_plugin(code,name,type,version,author,status,expire) VALUES('guoguo',?,'template','1.0.0','SuxinVideo',1,0)", "果果剧库")
}
