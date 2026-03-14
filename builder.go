package multipartbuilder

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
)

var (
	emptyFieldName = errors.New("empty fieldName")
	emptyValue     = errors.New("empty value")
	emptyData      = errors.New("empty data")
)

type builder struct {
	buffer *bytes.Buffer
	writer *multipart.Writer
	errors map[string]error
	err    error
}

// New creates a new multipart request builder with an in-memory buffer.
func New() *builder {
	buffer := bytes.NewBuffer(nil)
	writer := multipart.NewWriter(buffer)
	errs := make(map[string]error)

	return &builder{
		buffer: buffer,
		writer: writer,
		errors: errs,
	}
}

// WithField adds a field to the multipart request.
func (b *builder) WithField(fieldName, value string, required ...bool) *builder {
	if b.err != nil {
		return b
	}

	if fieldName == "" {
		b.err = emptyFieldName
		return b
	}

	fixErr := fieldRequired(required)

	if value == "" {
		if fixErr {
			b.err = fmt.Errorf("%s is required", fieldName)
			return b
		}
		b.errors[fieldName] = emptyValue
		return b
	}

	err := b.writer.WriteField(fieldName, value)
	if err != nil {
		err = fmt.Errorf("failed to write %s: %w", fieldName, err)
		if fixErr {
			b.err = err
			return b
		}
		b.errors[fieldName] = err
	}

	return b
}

// WithBytes adds raw byte data to the multipart request as a field.
func (b *builder) WithBytes(fieldName string, data []byte, required ...bool) *builder {
	if b.err != nil {
		return b
	}

	if fieldName == "" {
		b.err = emptyFieldName
		return b
	}

	fixErr := fieldRequired(required)

	if len(data) == 0 {
		if fixErr {
			b.err = fmt.Errorf("%w for %s", emptyData, fieldName)
			return b
		}
		b.errors[fieldName] = emptyData
		return b
	}

	part, err := b.writer.CreateFormField(fieldName)
	if err != nil {
		err = fmt.Errorf("failed to create form for %s: %w", fieldName, err)
		if fixErr {
			b.err = err
			return b
		}
		b.errors[fieldName] = err
		return b
	}

	_, err = part.Write(data)
	if err != nil {
		err = fmt.Errorf("failed to write data for %s: %w", fieldName, err)
		if fixErr {
			b.err = err
			return b
		}
		b.errors[fieldName] = err
	}

	return b
}

// WithAnyMarshaled marshals the given object to JSON and adds it as a field.
func (b *builder) WithAnyMarshaled(fieldName string, value interface{}, required ...bool) *builder {
	if b.err != nil {
		return b
	}

	if fieldName == "" {
		b.err = emptyFieldName
		return b
	}

	fixErr := fieldRequired(required)

	data, err := json.Marshal(value)
	if err != nil {
		if fixErr {
			b.err = err
		}
		b.errors[fieldName] = err
		return b
	}

	return b.WithBytes(fieldName, data, required...)
}

// WithFile adds a file to the multipart request from an io.Reader.
// Optionally, a custom Content-Type can be set.
func (b *builder) WithFile(fieldName, filename string, r io.Reader, contentType string, required ...bool) *builder {
	if b.err != nil {
		return b
	}

	if fieldName == "" {
		b.err = emptyFieldName
		return b
	}

	fixErr := fieldRequired(required)

	if filename == "" {
		err := fmt.Errorf("filename is required for %s", fieldName)
		if fixErr {
			b.err = err
			return b
		}
		b.errors[fieldName] = err
		return b
	}

	if r == nil {
		err := fmt.Errorf("empty reader for %s", fieldName)
		if fixErr {
			b.err = err
			return b
		}
		b.errors[fieldName] = err
		return b
	}

	var (
		part io.Writer
		err  error
	)

	if contentType == "" {
		part, err = b.writer.CreateFormFile(fieldName, filename)
	} else {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, filename))
		h.Set("Content-Type", contentType)
		part, err = b.writer.CreatePart(h)
	}

	if err != nil {
		err = fmt.Errorf("failed to create form part %s: %w", fieldName, err)
		if fixErr {
			b.err = err
			return b
		}
		b.errors[fieldName] = err
		return b
	}

	if _, err = io.Copy(part, r); err != nil {
		err = fmt.Errorf("failed to copy file data for %s: %w", fieldName, err)
		if fixErr {
			b.err = err
			return b
		}
		b.errors[fieldName] = err
	}

	return b
}

// WithFileBytes adds a file from a byte slice.
func (b *builder) WithFileBytes(fieldName, filename string, data []byte, required ...bool) *builder {
	if b.err != nil {
		return b
	}

	if fieldName == "" {
		b.err = emptyFieldName
		return b
	}

	if len(data) == 0 {
		if fieldRequired(required) {
			b.err = fmt.Errorf("%w for %s", emptyData, fieldName)
			return b
		}
		b.errors[fieldName] = emptyData
		return b
	}

	return b.WithFile(fieldName, filename, bytes.NewReader(data), "", required...)
}

// Build finalizes the multipart request build.
// Non-required field errors are stored in Errors() and do not fail the build.
func (b *builder) Build() (data *bytes.Buffer, contentType string, err error) {
	if b.err != nil {
		return nil, "", b.err
	}

	if err = b.writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	return b.buffer, b.writer.FormDataContentType(), nil
}

// HasErrors returns true if non-required fields errors were stored
func (b *builder) HasErrors() bool {
	return len(b.errors) > 0
}

// Errors returns map[string]error with ignored errors by field names.
func (b *builder) Errors() map[string]error {
	out := make(map[string]error, len(b.errors))
	for k, v := range b.errors {
		out[k] = v
	}
	return out
}

// Reset clears the builder state, buffer and errors map allowing it to be reused.
func (b *builder) Reset() {
	b.buffer.Reset()
	b.writer = multipart.NewWriter(b.buffer)
	b.errors = make(map[string]error)
	b.err = nil
}

func fieldRequired(required []bool) bool {
	if len(required) == 0 {
		return true
	}
	return required[0]
}
