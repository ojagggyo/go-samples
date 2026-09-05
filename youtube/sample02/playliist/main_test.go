package main

import "testing"

func TestVideoID(t *testing.T) {
	tests := map[string]string{
		"https://youtu.be/dQw4w9WgXcQ?si=abc":         "dQw4w9WgXcQ",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ": "dQw4w9WgXcQ",
		"https://youtube.com/shorts/dQw4w9WgXcQ":      "dQw4w9WgXcQ",
		"https://m.youtube.com/embed/dQw4w9WgXcQ":     "dQw4w9WgXcQ",
	}
	for rawURL, want := range tests {
		got, err := videoID(rawURL)
		if err != nil || got != want {
			t.Errorf("videoID(%q) = %q, %v; want %q, nil", rawURL, got, err, want)
		}
	}
}

func TestUniqueVideoIDs(t *testing.T) {
	ids, invalid := uniqueVideoIDs([]string{
		"https://youtu.be/dQw4w9WgXcQ",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://example.com/watch?v=dQw4w9WgXcQ",
	})
	if len(ids) != 1 || ids[0] != "dQw4w9WgXcQ" || len(invalid) != 1 {
		t.Fatalf("ids=%v invalid=%v", ids, invalid)
	}
}
