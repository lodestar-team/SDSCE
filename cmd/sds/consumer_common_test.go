package main

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	sds "github.com/graphprotocol/substreams-data-service"
)

func TestParseDeadline(t *testing.T) {
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		raw  string
		want uint64
	}{
		{name: "duration", raw: "1h", want: uint64(now.Add(time.Hour).Unix())},
		{name: "unix", raw: "1778112000", want: 1778112000},
		{name: "rfc3339", raw: "2026-05-07T00:00:00Z", want: 1778112000},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseDeadline(test.raw, now)
			if err != nil {
				t.Fatalf("parseDeadline() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("parseDeadline() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestParseDeadlineRejectsNonPositiveDuration(t *testing.T) {
	_, err := parseDeadline("0s", time.Now())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseHexBytesPadsOddLength(t *testing.T) {
	got, err := parseHexBytes("0xabc")
	if err != nil {
		t.Fatalf("parseHexBytes() error = %v", err)
	}
	if len(got) != 2 || got[0] != 0x0a || got[1] != 0xbc {
		t.Fatalf("parseHexBytes() = %x, want 0abc", got)
	}
}

func TestCommonToEthAddressCopiesBytes(t *testing.T) {
	address := common.HexToAddress("0x1111111111111111111111111111111111111111")
	got := commonToEthAddress(address)
	if len(got) != common.AddressLength {
		t.Fatalf("len(commonToEthAddress()) = %d, want %d", len(got), common.AddressLength)
	}
	if got.Pretty() != address.Hex() {
		t.Fatalf("commonToEthAddress() = %s, want %s", got.Pretty(), address.Hex())
	}
}

func TestRejectAllowanceReplacement(t *testing.T) {
	ten := sds.MustNewGRT("10 GRT")
	five := sds.MustNewGRT("5 GRT")
	zero := sds.ZeroGRT()

	if !rejectAllowanceReplacement(ten, five, false, false) {
		t.Fatal("expected non-zero to non-zero replacement to be rejected")
	}
	if rejectAllowanceReplacement(ten, five, true, false) {
		t.Fatal("expected --force to allow replacement")
	}
	if rejectAllowanceReplacement(ten, five, false, true) {
		t.Fatal("expected --reset-first to allow replacement")
	}
	if rejectAllowanceReplacement(zero, five, false, false) {
		t.Fatal("expected zero to non-zero replacement to be allowed")
	}
	if rejectAllowanceReplacement(ten, zero, false, false) {
		t.Fatal("expected non-zero to zero replacement to be allowed")
	}
}

func TestTopUpDepositAmount(t *testing.T) {
	current := sds.MustNewGRT("3 GRT")
	target := sds.MustNewGRT("10 GRT")

	deposit, needed := topUpDepositAmount(current, target)
	if !needed {
		t.Fatal("expected top-up to be needed")
	}
	if deposit.String() != "7 GRT" {
		t.Fatalf("deposit = %s, want 7 GRT", deposit.String())
	}

	deposit, needed = topUpDepositAmount(target, current)
	if needed {
		t.Fatal("expected no top-up when current balance is above target")
	}
	if !deposit.IsZero() {
		t.Fatalf("deposit = %s, want zero", deposit.String())
	}
}
