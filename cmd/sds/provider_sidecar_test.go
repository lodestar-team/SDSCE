package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseDevAcceptedSigners(t *testing.T) {
	addrs, err := parseDevAcceptedSigners("")
	require.NoError(t, err)
	require.Empty(t, addrs)

	addrs, err = parseDevAcceptedSigners(" 0x1111111111111111111111111111111111111111 ")
	require.NoError(t, err)
	require.Len(t, addrs, 1)
	require.Equal(t, "0x1111111111111111111111111111111111111111", addrs[0].Pretty())

	_, err = parseDevAcceptedSigners("not-an-address")
	require.Error(t, err)
}
