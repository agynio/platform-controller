package controller

import (
	"errors"
	"testing"

	"connectrpc.com/connect"
)

// A denial is a race with Identity granting this controller cluster admin, not a
// verdict on the declaration. Classifying it permanent stranded every object
// reconciled in the seconds before that grant landed, and nothing retried them.
func TestPermanent(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "an invalid declaration is permanent",
			err:  connect.NewError(connect.CodeInvalidArgument, errors.New("slug")),
			want: true,
		},
		{
			name: "so is a name already taken",
			err:  connect.NewError(connect.CodeAlreadyExists, errors.New("taken")),
			want: true,
		},
		{
			name: "a denial is not, the grant converges at startup",
			err:  connect.NewError(connect.CodePermissionDenied, errors.New("permission denied")),
			want: false,
		},
		{
			name: "nor is an unreachable platform",
			err:  connect.NewError(connect.CodeUnavailable, errors.New("connection refused")),
			want: false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := permanent(testCase.err); got != testCase.want {
				t.Fatalf("expected %v, got %v", testCase.want, got)
			}
		})
	}
}
