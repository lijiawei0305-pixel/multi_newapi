package common

// ControlPlaneJSONMaxBytes is the hard response-body limit for OAuth,
// discovery, metadata, and other non-relay control-plane JSON endpoints.
// Provider inference responses use the separately configurable relay limit.
const ControlPlaneJSONMaxBytes int64 = 4 << 20
