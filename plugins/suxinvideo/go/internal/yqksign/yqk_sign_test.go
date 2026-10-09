package yqksign

import "testing"

// These synthetic vectors contain no captured device, account or session data.
func TestRawValuesAndFiltering(t *testing.T) {
	params := map[string]any{"sign": "old", "empty": "", "null": nil, "zero": 0, "space": " ", "word": "仙逆 &x=1", "list": "[]"}
	got, err := SigningText(params)
	if err != nil {
		t.Fatal("signing text failed")
	}
	want := "list=[]&space= &word=仙逆 &x=1&zero=0&appKey=" + AppKey
	if got != want {
		t.Fatal("incorrect raw formatting or filtering")
	}
	if _, err := GenerateSign(map[string]any{"list": []int{1}}); err == nil {
		t.Fatal("native arrays require explicit serialization")
	}
	if _, err := GenerateSign(map[string]any{"float": 1.5}); err == nil {
		t.Fatal("unverified floating-point formatting must not be accepted")
	}
	first, err := SignRequest(params)
	if err != nil || first["sign"] == "old" || params["sign"] != "old" {
		t.Fatal("request signing did not replace signature in an independent copy")
	}
	second, err := SignRequest(map[string]any{"zero": 0, "space": " ", "word": "仙逆 &x=1", "list": "[]"})
	if err != nil || first["sign"] != second["sign"] {
		t.Fatal("field order/empty values altered the signature")
	}
}
