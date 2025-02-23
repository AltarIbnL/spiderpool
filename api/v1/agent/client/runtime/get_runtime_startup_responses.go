// Code generated by go-swagger; DO NOT EDIT.

// Copyright 2022 Authors of spidernet-io
// SPDX-License-Identifier: Apache-2.0

package runtime

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"fmt"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
)

// GetRuntimeStartupReader is a Reader for the GetRuntimeStartup structure.
type GetRuntimeStartupReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *GetRuntimeStartupReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {
	case 200:
		result := NewGetRuntimeStartupOK()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil
	case 500:
		result := NewGetRuntimeStartupInternalServerError()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	default:
		return nil, runtime.NewAPIError("response status code does not match any response statuses defined for this endpoint in the swagger spec", response, response.Code())
	}
}

// NewGetRuntimeStartupOK creates a GetRuntimeStartupOK with default headers values
func NewGetRuntimeStartupOK() *GetRuntimeStartupOK {
	return &GetRuntimeStartupOK{}
}

/*
GetRuntimeStartupOK describes a response with status code 200, with default header values.

Success
*/
type GetRuntimeStartupOK struct {
}

func (o *GetRuntimeStartupOK) Error() string {
	return fmt.Sprintf("[GET /runtime/startup][%d] getRuntimeStartupOK ", 200)
}

func (o *GetRuntimeStartupOK) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewGetRuntimeStartupInternalServerError creates a GetRuntimeStartupInternalServerError with default headers values
func NewGetRuntimeStartupInternalServerError() *GetRuntimeStartupInternalServerError {
	return &GetRuntimeStartupInternalServerError{}
}

/*
GetRuntimeStartupInternalServerError describes a response with status code 500, with default header values.

Failed
*/
type GetRuntimeStartupInternalServerError struct {
}

func (o *GetRuntimeStartupInternalServerError) Error() string {
	return fmt.Sprintf("[GET /runtime/startup][%d] getRuntimeStartupInternalServerError ", 500)
}

func (o *GetRuntimeStartupInternalServerError) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}
