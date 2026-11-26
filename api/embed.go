// Package api хранит OpenAPI-спецификацию, которая показывается в Swagger UI.
package api

import _ "embed"

//go:embed openapi.yaml
var OpenAPI []byte
