package peyeeye

import "context"

// ListEntities returns the built-in catalog and the org's custom detectors.
func (c *Client) ListEntities(ctx context.Context) (*EntitiesList, error) {
	var out EntitiesList
	err := c.do(ctx, requestOpts{method: "GET", path: "/v1/entities"}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateEntityParams is the body of POST /v1/entities. ID is required; Kind
// defaults to "regex". When Pattern is empty the server induces one from
// Examples at create time.
type CreateEntityParams struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind,omitempty"`
	Pattern         string   `json:"pattern,omitempty"`
	Examples        []string `json:"examples,omitempty"`
	ConfidenceFloor *float64 `json:"confidence_floor,omitempty"`
}

// CreateEntity registers (or upserts) a custom detector.
func (c *Client) CreateEntity(ctx context.Context, p CreateEntityParams) (*CustomDetector, error) {
	if p.Kind == "" {
		p.Kind = "regex"
	}
	var out CustomDetector
	err := c.do(ctx, requestOpts{
		method: "POST",
		path:   "/v1/entities",
		body:   p,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateEntityParams is the body of PATCH /v1/entities/:id. All fields are
// optional — only those that are non-nil are sent.
type UpdateEntityParams struct {
	Pattern         *string  `json:"pattern,omitempty"`
	Enabled         *bool    `json:"enabled,omitempty"`
	ConfidenceFloor *float64 `json:"confidence_floor,omitempty"`
}

// UpdateEntity partially updates a custom detector.
func (c *Client) UpdateEntity(ctx context.Context, entityID string, p UpdateEntityParams) (*CustomDetector, error) {
	var out CustomDetector
	err := c.do(ctx, requestOpts{
		method: "PATCH",
		path:   "/v1/entities/" + entityID,
		body:   p,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteEntity retires a custom detector.
func (c *Client) DeleteEntity(ctx context.Context, entityID string) error {
	return c.do(ctx, requestOpts{
		method:     "DELETE",
		path:       "/v1/entities/" + entityID,
		allowEmpty: true,
	}, nil)
}

// TestPattern dry-runs a regex against sample text without saving anything.
func (c *Client) TestPattern(ctx context.Context, pattern, text string) (*TestPatternResponse, error) {
	body := struct {
		Pattern string `json:"pattern"`
		Text    string `json:"text"`
	}{Pattern: pattern, Text: text}
	var out TestPatternResponse
	err := c.do(ctx, requestOpts{
		method: "POST",
		path:   "/v1/entities/test",
		body:   body,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// EntityTemplates returns the starter-detector catalog.
func (c *Client) EntityTemplates(ctx context.Context) ([]EntityTemplate, error) {
	var env struct {
		Templates []EntityTemplate `json:"templates"`
	}
	err := c.do(ctx, requestOpts{method: "GET", path: "/v1/entities/templates"}, &env)
	if err != nil {
		return nil, err
	}
	return env.Templates, nil
}
