package controller

// Payment, balance, and other control-plane responses are expected to be
// compact JSON. Keep their bound below the configurable task/relay JSON cap.
const upstreamControlPlaneResponseMaxBytes = int64(4 << 20)
