package application

// fakeRelayTransport bundles three fakeTransports (one per relay channel) behind
// the RelayTransport port, so a test can script inbound frames on the coordinator
// channel and inspect what the session sent — reusing the existing fakeTransport
// (its Send/Receive satisfy RelayChannel structurally; its extra Close is unused
// here since the bundle owns lifecycle).
type fakeRelayTransport struct {
	coordinator *fakeTransport
	media       *fakeTransport
	storage     *fakeTransport
}

// Compile-time proof the fake satisfies the port (and, transitively, that
// *fakeTransport satisfies RelayChannel).
var _ RelayTransport = (*fakeRelayTransport)(nil)

func newFakeRelayTransport() *fakeRelayTransport {
	return &fakeRelayTransport{
		coordinator: newFakeTransport(),
		media:       newFakeTransport(),
		storage:     newFakeTransport(),
	}
}

func (f *fakeRelayTransport) Coordinator() RelayChannel { return f.coordinator }
func (f *fakeRelayTransport) Media() RelayChannel       { return f.media }
func (f *fakeRelayTransport) Storage() RelayChannel     { return f.storage }

func (f *fakeRelayTransport) Close() error {
	_ = f.coordinator.Close()
	_ = f.media.Close()
	_ = f.storage.Close()
	return nil
}
