// Package api holds the HTTP contract of the service.
package api

import _ "embed"

// OpenAPI is the OpenAPI 3.1 specification of the HTTP API.
//
//go:embed openapi.yaml
var OpenAPI []byte

// SwaggerUI is the page that renders OpenAPI with Swagger UI.
//
//go:embed swagger.html
var SwaggerUI []byte
