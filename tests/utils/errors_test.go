package utils_test

import (
	"errors"
	"testing"

	"jf/internal/utils"
)

func TestAppError(t *testing.T) {
	tests := []struct {
		name        string
		errType     utils.ErrorType
		message     string
		originalErr error
		expectedMsg string
	}{
		{
			name:        "NetworkError with original error",
			errType:     utils.ErrorTypeNetwork,
			message:     "failed to connect",
			originalErr: errors.New("connection refused"),
			expectedMsg: "failed to connect: connection refused",
		},
		{
			name:        "NetworkError without original error",
			errType:     utils.ErrorTypeNetwork,
			message:     "connection timeout",
			originalErr: nil,
			expectedMsg: "connection timeout",
		},
		{
			name:        "DatabaseError",
			errType:     utils.ErrorTypeDatabase,
			message:     "query failed",
			originalErr: errors.New("syntax error"),
			expectedMsg: "query failed: syntax error",
		},
		{
			name:        "ParsingError",
			errType:     utils.ErrorTypeParsing,
			message:     "invalid JSON",
			originalErr: errors.New("unexpected EOF"),
			expectedMsg: "invalid JSON: unexpected EOF",
		},
		{
			name:        "ValidationError",
			errType:     utils.ErrorTypeValidation,
			message:     "invalid input",
			originalErr: errors.New("required field missing"),
			expectedMsg: "invalid input: required field missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := utils.NewAppError(tt.errType, tt.message, tt.originalErr)

			if err.Type != tt.errType {
				t.Errorf("Expected error type %v, got %v", tt.errType, err.Type)
			}

			if err.Message != tt.message {
				t.Errorf("Expected message %q, got %q", tt.message, err.Message)
			}

			if err.Cause != tt.originalErr {
				t.Errorf("Expected original error %v, got %v", tt.originalErr, err.Cause)
			}

			if err.Error() != tt.expectedMsg {
				t.Errorf("Expected error string %q, got %q", tt.expectedMsg, err.Error())
			}

			if err.Timestamp == "" {
				t.Error("Expected error timestamp to be set")
			}

			if len(err.Stack) == 0 {
				t.Error("Expected stack trace to be captured")
			}
		})
	}
}

func TestSpecificErrorConstructors(t *testing.T) {
	t.Run("NewNetworkError", func(t *testing.T) {
		err := utils.NewNetworkError("connection failed", errors.New("timeout"))

		if err.Type != utils.ErrorTypeNetwork {
			t.Errorf("Expected NetworkError, got %v", err.Type)
		}

		if err.Message != "connection failed" {
			t.Errorf("Expected message 'connection failed', got %q", err.Message)
		}

		if err.Cause.Error() != "timeout" {
			t.Errorf("Expected original error 'timeout', got %q", err.Cause.Error())
		}
	})

	t.Run("NewDatabaseError", func(t *testing.T) {
		err := utils.NewDatabaseError("query failed", errors.New("syntax error"))

		if err.Type != utils.ErrorTypeDatabase {
			t.Errorf("Expected DatabaseError, got %v", err.Type)
		}

		if err.Message != "query failed" {
			t.Errorf("Expected message 'query failed', got %q", err.Message)
		}
	})

	t.Run("NewParsingError", func(t *testing.T) {
		err := utils.NewParsingError("invalid format", errors.New("unexpected token"))

		if err.Type != utils.ErrorTypeParsing {
			t.Errorf("Expected ParsingError, got %v", err.Type)
		}

		if err.Message != "invalid format" {
			t.Errorf("Expected message 'invalid format', got %q", err.Message)
		}
	})
}

func TestAppErrorUnwrap(t *testing.T) {
	originalErr := errors.New("original error")
	appErr := utils.NewAppError(utils.ErrorTypeNetwork, "wrapped error", originalErr)

	unwrapped := appErr.Unwrap()
	if unwrapped != originalErr {
		t.Errorf("Expected unwrapped error %v, got %v", originalErr, unwrapped)
	}

	// Test with nil original error
	appErr2 := utils.NewAppError(utils.ErrorTypeNetwork, "no original error", nil)
	unwrapped2 := appErr2.Unwrap()
	if unwrapped2 != nil {
		t.Errorf("Expected nil unwrapped error, got %v", unwrapped2)
	}
}

func TestErrorTypeString(t *testing.T) {
	tests := []struct {
		errType  utils.ErrorType
		expected string
	}{
		{utils.ErrorTypeUnknown, "UnknownError"},
		{utils.ErrorTypeNetwork, "NetworkError"},
		{utils.ErrorTypeDatabase, "DatabaseError"},
		{utils.ErrorTypeParsing, "ParsingError"},
		{utils.ErrorTypeValidation, "ValidationError"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			// This is a basic test - in a real implementation you might want
			// to add a String() method to ErrorType
			if tt.errType < utils.ErrorTypeUnknown || tt.errType > utils.ErrorTypeValidation {
				t.Errorf("ErrorType %v is out of expected range", tt.errType)
			}
		})
	}
}

func TestAppErrorFields(t *testing.T) {
	err := utils.NewAppError(utils.ErrorTypeNetwork, "test error", errors.New("original"))

	// Test that all fields are properly set
	if err.Type != utils.ErrorTypeNetwork {
		t.Error("Type field not set correctly")
	}

	if err.Message != "test error" {
		t.Error("Message field not set correctly")
	}

	if err.Cause == nil {
		t.Error("Cause field not set correctly")
	}

	if err.Timestamp == "" {
		t.Error("Timestamp field not set correctly")
	}

	if len(err.Stack) == 0 {
		t.Error("Stack field not set correctly")
	}
}

func TestAppErrorStackTrace(t *testing.T) {
	err := utils.NewAppError(utils.ErrorTypeNetwork, "test error", nil)

	// Test that stack trace contains expected function names
	stackStr := string(err.Stack)
	if stackStr == "" {
		t.Error("Stack trace is empty")
	}

	// The stack should contain this test function name
	if !contains(stackStr, "TestAppErrorStackTrace") {
		t.Error("Stack trace should contain test function name")
	}
}

func TestAppErrorErrorMethod(t *testing.T) {
	tests := []struct {
		name     string
		err      *utils.AppError
		expected string
	}{
		{
			name:     "with original error",
			err:      utils.NewAppError(utils.ErrorTypeNetwork, "network failed", errors.New("timeout")),
			expected: "network failed: timeout",
		},
		{
			name:     "without original error",
			err:      utils.NewAppError(utils.ErrorTypeDatabase, "database failed", nil),
			expected: "database failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, tt.err.Error())
			}
		})
	}
}

func TestErrorTypeConstants(t *testing.T) {
	// Test that error type constants are properly defined
	if utils.ErrorTypeUnknown != 0 {
		t.Errorf("Expected ErrorTypeUnknown to be 0, got %d", utils.ErrorTypeUnknown)
	}

	if utils.ErrorTypeNetwork != 1 {
		t.Errorf("Expected ErrorTypeNetwork to be 1, got %d", utils.ErrorTypeNetwork)
	}

	if utils.ErrorTypeDatabase != 2 {
		t.Errorf("Expected ErrorTypeDatabase to be 2, got %d", utils.ErrorTypeDatabase)
	}

	if utils.ErrorTypeParsing != 3 {
		t.Errorf("Expected ErrorTypeParsing to be 3, got %d", utils.ErrorTypeParsing)
	}

	if utils.ErrorTypeValidation != 4 {
		t.Errorf("Expected ErrorTypeValidation to be 4, got %d", utils.ErrorTypeValidation)
	}
}

// Helper function for string contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr ||
		len(s) > len(substr) && contains(s[1:], substr)
}

func BenchmarkAppErrorCreation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = utils.NewAppError(utils.ErrorTypeNetwork, "test error", errors.New("original error"))
	}
}

func BenchmarkAppErrorString(b *testing.B) {
	err := utils.NewAppError(utils.ErrorTypeNetwork, "test error", errors.New("original error"))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = err.Error()
	}
}
