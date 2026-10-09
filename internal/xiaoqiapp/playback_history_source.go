package app

func playbackHistoryIdentity(id string) (string, string, bool) {
	source, sourceID, valid := splitProviderDramaID(id)
	if !valid {
		return "", "", false
	}
	return providerDramaID(source, sourceID), source, true
}

// hongguoHistoryIdentity keeps legacy following/Emby records restricted to
// the provider format those exports historically supported. Online playback
// history itself accepts every normalized provider ID through the function
// above.
func hongguoHistoryIdentity(id string) (string, string, bool) {
	canonical, source, valid := playbackHistoryIdentity(id)
	if !valid || source != sourceHongguo {
		return "", "", false
	}
	return canonical, source, true
}
