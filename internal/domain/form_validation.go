package domain

const (
	FormErrorTaskStatusNotInList = "FORM_TASK_STATUS_NOT_IN_LIST"
	FormErrorTaskTemplateInvalid = "FORM_TASK_TEMPLATE_INVALID"
)

type FormValidationError struct {
	Code   string
	Field  string
	Reason string
}

func NewFormValidationError(field, reason string) *FormValidationError {
	return &FormValidationError{Field: field, Reason: reason}
}

func (e *FormValidationError) WithCode(code string) *FormValidationError {
	copied := *e
	copied.Code = code
	return &copied
}

func (e *FormValidationError) WithPrefix(prefix string) *FormValidationError {
	copied := *e
	copied.Field = prefix + copied.Field
	return &copied
}

func (e *FormValidationError) Error() string {
	if e.Field == "" {
		return ErrInvalidForm.Error() + ": " + e.Reason
	}
	return ErrInvalidForm.Error() + ": " + e.Field + ": " + e.Reason
}

func (e *FormValidationError) Unwrap() error { return ErrInvalidForm }
