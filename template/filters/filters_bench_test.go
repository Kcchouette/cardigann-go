package filters

import "testing"

// BenchmarkFuzzyTime measures the fuzzytime filter on a value that only
// matches in the final fuzzy-layout pass (11 dateLayouts + 6 fuzzyLayouts
// attempts — the worst realistic case).
func BenchmarkFuzzyTime(b *testing.B) {
	steps := []Step{{Name: "fuzzytime"}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Apply("Jan 2 '06 15:04", steps); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDateParse measures the dateparse filter on an RFC3339 value
// (matches on the 3rd of 11 layouts).
func BenchmarkDateParse(b *testing.B) {
	steps := []Step{{Name: "dateparse"}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Apply("2024-01-02T15:04:05Z", steps); err != nil {
			b.Fatal(err)
		}
	}
}
