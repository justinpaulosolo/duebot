package gateway

const (
	OpDispatch            = 0 // Server -> client: an event happened
	OpHeartbeat           = 1 // Both ways: keep the connection alive
	OpIdentify            = 2 // Client -> server: start a new session
	OpPresenceUpdate      = 3
	OpVoiceStateUpdate    = 4
	OpResume              = 6 // Client -> server: resume a dropped session
	OpReconnect           = 7 // Server -> client: reconnect and resume
	OpRequestGuildMembers = 8
	OpInvalidSession      = 9  // Server -> client: session is dead, check d for resumability
	OpHello               = 10 // Server -> client: sent immediately on connect, carries heartbeat_interval
	OpHeartbeatACK        = 11 // Server -> client: acknowledges our last heartbeat
)

const (
	IntentGuilds         = 1 << 0
	IntentGuildMessages  = 1 << 9
	IntentDirectMessages = 1 << 12
	IntentMessageContent = 1 << 15
)
