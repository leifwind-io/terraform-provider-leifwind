package leifwindtest

import (
	"fmt"
	"strings"
)

// stackContractMajor is the major version of the stack.env / LW_TEST_* attach
// contract this package speaks. It must track the backend's
// stack/contract.py STACK_CONTRACT_VERSION.
const stackContractMajor = "1"

// ContractError reports a missing or version-incompatible LW_TEST_* attach
// environment. It mirrors the backend's StackContractError: a misconfigured
// attach run must fail with a clear contract error, not a nil-field panic.
type ContractError struct{ Msg string }

func (e *ContractError) Error() string { return e.Msg }

func contractErrorf(format string, args ...any) *ContractError {
	return &ContractError{Msg: fmt.Sprintf(format, args...)}
}

// requireEnv fetches key via getenv, treating empty as missing (Go cannot
// distinguish unset from empty; no contract value is legitimately empty).
func requireEnv(getenv func(string) string, key string) (string, error) {
	v := getenv(key)
	if v == "" {
		return "", contractErrorf(
			"%s is missing from the LW_TEST_* attach environment — source a complete stack.env written by `make stack-seed`",
			key)
	}
	return v, nil
}

func checkContractVersion(getenv func(string) string) error {
	version := getenv("LW_STACK_CONTRACT_VERSION")
	if version == "" {
		return contractErrorf(
			"LW_TEST_* attach variables are set but LW_STACK_CONTRACT_VERSION is missing — source a stack.env written by `make stack-seed`")
	}
	if major, _, _ := strings.Cut(version, "."); major != stackContractMajor {
		return contractErrorf(
			"stack.env contract version %s is incompatible with this consumer (speaks major %s)",
			version, stackContractMajor)
	}
	return nil
}
