package utils

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

// ErrorType represents different types of errors in the application
type ErrorType int

const (
	ErrorTypeUnknown ErrorType = iota
	ErrorTypeNetwork
	ErrorTypeDatabase
	ErrorTypeParsing
	ErrorTypeValidation
	ErrorTypeTimeout
	ErrorTypeConfiguration
	ErrorTypeResource
)

// AppError represents an application-specific error with additional context
type AppError struct {
	Type      ErrorType
	Message   string
	Cause     error
	Context   map[string]interface{}
	Stack     []byte
	Timestamp string
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

// Unwrap returns the underlying error
func (e *AppError) Unwrap() error {
	return e.Cause
}

// NewAppError creates a new application error
func NewAppError(errType ErrorType, message string, cause error) *AppError {
	return &AppError{
		Type:      errType,
		Message:   message,
		Cause:     cause,
		Context:   make(map[string]interface{}),
		Stack:     getStackTrace(),
		Timestamp: getCurrentTimestamp(),
	}
}

// WithContext adds context to the error
func (e *AppError) WithContext(key string, value interface{}) *AppError {
	e.Context[key] = value
	return e
}

// WithContexts adds multiple contexts to the error
func (e *AppError) WithContexts(contexts map[string]interface{}) *AppError {
	for k, v := range contexts {
		e.Context[k] = v
	}
	return e
}

// IsType checks if the error is of a specific type
func (e *AppError) IsType(errType ErrorType) bool {
	return e.Type == errType
}

// Convenience functions for creating specific error types
func NewNetworkError(message string, cause error) *AppError {
	return NewAppError(ErrorTypeNetwork, message, cause)
}

func NewDatabaseError(message string, cause error) *AppError {
	return NewAppError(ErrorTypeDatabase, message, cause)
}

func NewParsingError(message string, cause error) *AppError {
	return NewAppError(ErrorTypeParsing, message, cause)
}

func NewValidationError(message string, cause error) *AppError {
	return NewAppError(ErrorTypeValidation, message, cause)
}

func NewTimeoutError(message string, cause error) *AppError {
	return NewAppError(ErrorTypeTimeout, message, cause)
}

func NewConfigurationError(message string, cause error) *AppError {
	return NewAppError(ErrorTypeConfiguration, message, cause)
}

func NewResourceError(message string, cause error) *AppError {
	return NewAppError(ErrorTypeResource, message, cause)
}

// ErrorTypeString returns a human-readable string for the error type
func (et ErrorType) String() string {
	switch et {
	case ErrorTypeNetwork:
		return "Network"
	case ErrorTypeDatabase:
		return "Database"
	case ErrorTypeParsing:
		return "Parsing"
	case ErrorTypeValidation:
		return "Validation"
	case ErrorTypeTimeout:
		return "Timeout"
	case ErrorTypeConfiguration:
		return "Configuration"
	case ErrorTypeResource:
		return "Resource"
	default:
		return "Unknown"
	}
}

// getStackTrace captures the current stack trace
func getStackTrace() []byte {
	buf := make([]byte, 1024)
	n := runtime.Stack(buf, false)
	return buf[:n]
}

// getCurrentTimestamp returns the current timestamp as a string
func getCurrentTimestamp() string {
	return fmt.Sprintf("%d", time.Now().Unix())
}

// SafeClose safely closes a resource with error handling
func SafeClose(closer interface{ Close() error }, resourceName string) {
	if closer != nil {
		if err := closer.Close(); err != nil {
			// Log the error but don't panic
			fmt.Printf("Error closing %s: %v\n", resourceName, err)
		}
	}
}

// SafeCloseWithError safely closes a resource and returns the error
func SafeCloseWithError(closer interface{ Close() error }, resourceName string) error {
	if closer != nil {
		if err := closer.Close(); err != nil {
			return NewResourceError(fmt.Sprintf("failed to close %s", resourceName), err)
		}
	}
	return nil
}

// RecoverFromPanic recovers from a panic and returns an error
func RecoverFromPanic(operation string) error {
	if r := recover(); r != nil {
		return NewAppError(ErrorTypeUnknown,
			fmt.Sprintf("panic in %s", operation),
			fmt.Errorf("panic: %v", r))
	}
	return nil
}

// WrapError wraps an existing error with additional context
func WrapError(err error, message string) error {
	if err == nil {
		return nil
	}

	if appErr, ok := err.(*AppError); ok {
		return appErr.WithContext("wrapped_message", message)
	}

	return NewAppError(ErrorTypeUnknown, message, err)
}

// IsRetryableError checks if an error is retryable
func IsRetryableError(err error) bool {
	if appErr, ok := err.(*AppError); ok {
		switch appErr.Type {
		case ErrorTypeNetwork, ErrorTypeTimeout:
			return true
		default:
			return false
		}
	}

	// Check for common retryable error patterns
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "temporary failure")
}

// ErrorSummary provides a summary of an error for logging
func ErrorSummary(err error) map[string]interface{} {
	if err == nil {
		return nil
	}

	summary := map[string]interface{}{
		"error": err.Error(),
	}

	if appErr, ok := err.(*AppError); ok {
		summary["type"] = appErr.Type.String()
		summary["timestamp"] = appErr.Timestamp
		if len(appErr.Context) > 0 {
			summary["context"] = appErr.Context
		}
	}

	return summary
}
