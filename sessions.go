package peyeeye

import "context"

// GetSession returns metadata for a stateful (ses_…) session.
func (c *Client) GetSession(ctx context.Context, sessionID string) (*SessionInfo, error) {
	var out SessionInfo
	err := c.do(ctx, requestOpts{
		method: "GET",
		path:   "/v1/sessions/" + sessionID,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSession evicts a stateful session immediately. It is a no-op for
// stateless skey_… blobs (which never had server-side state).
func (c *Client) DeleteSession(ctx context.Context, sessionID string) error {
	return c.do(ctx, requestOpts{
		method:     "DELETE",
		path:       "/v1/sessions/" + sessionID,
		allowEmpty: true,
	}, nil)
}
