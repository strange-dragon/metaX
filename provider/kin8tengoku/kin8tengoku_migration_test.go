package kin8tengoku

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeMovieIDMigration(t *testing.T) {
	k8 := New()
	tests := []struct {
		input string
		want  string
	}{
		{input: "3604", want: "3604"},
		{input: "kin8-1", want: "0001"},
		{input: "KIN8TENGOKU-0001", want: "0001"},
		{input: " KIN8_42 ", want: "0042"},
		{input: "heyzo-1234", want: ""},
		{input: "010115_001", want: ""},
		{input: "KIN8-0000", want: ""},
		{input: "KIN8-12345", want: ""},
		{input: "../1234", want: ""},
		{input: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			require.Equal(t, tt.want, k8.NormalizeMovieID(tt.input))
		})
	}
}

func TestGetMovieInfoByIDRejectsInvalidIDBeforeNetwork(t *testing.T) {
	_, err := New().GetMovieInfoByID("heyzo-1234")
	require.Error(t, err)
	_, err = New().GetMovieInfoByID("KIN8-0000")
	require.Error(t, err)
}
