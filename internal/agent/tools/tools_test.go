package tools

import (
	"errors"
	"testing"

	"github.com/liy0aay/family-expenses-bot/internal/agent"
)

func TestAmountArgAcceptsStringAndNumber(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    int64
		wantErr string
	}{
		{"number", `{"amount_rub": 123.0}`, 12300, ""},
		{"string", `{"amount_rub": "123.0"}`, 12300, ""},
		{"fraction", `{"amount_rub": 350.5}`, 35050, ""},
		{"three decimals", `{"amount_rub": 350.505}`, 0, "invalid_amount"},
		{"wrong type", `{"amount_rub": true}`, 0, "invalid_amount"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args, err := agent.ParseToolArguments(tc.raw)
			if err != nil {
				t.Fatal(err)
			}
			got, err := amountArg(args)
			if tc.wantErr != "" {
				var te *ToolError
				if !errors.As(err, &te) || te.Code != tc.wantErr {
					t.Fatalf("amountArg(%s) error = %v, want code %s", tc.raw, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("amountArg(%s): %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("amountArg(%s) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

func TestAmountArgMissing(t *testing.T) {
	args, err := agent.ParseToolArguments(`{}`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = amountArg(args)
	var te *ToolError
	if !errors.As(err, &te) || te.Code != "bad_request" {
		t.Fatalf("missing amount_rub error = %v, want code bad_request", err)
	}
}
