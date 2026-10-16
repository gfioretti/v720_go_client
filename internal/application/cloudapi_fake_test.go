package application

import (
	"context"

	"github.com/gfioretti/v720_go_client/internal/domain"
)

// fakeCloudAPI is a scriptable in-memory CloudAPI for exercising relay-session
// setup without a real REST backend. Tests set the canned return values and/or
// per-method errors, then inspect the recorded call arguments.
type fakeCloudAPI struct {
	account domain.Account
	devices []domain.Device
	conf    domain.LiveConf

	loginErr    error
	devicesErr  error
	watchingErr error
	confErr     error

	// Recorded arguments, for asserting the session called the port correctly.
	loginUser     string
	loginPass     string
	watchingCode  string
	watchingPwd   string
	watchingCalls int
	confDevID     string
}

// Compile-time proof the fake satisfies the port it stands in for.
var _ CloudAPI = (*fakeCloudAPI)(nil)

func (f *fakeCloudAPI) Login(_ context.Context, username, password string) (domain.Account, error) {
	f.loginUser, f.loginPass = username, password
	if f.loginErr != nil {
		return domain.Account{}, f.loginErr
	}
	return f.account, nil
}

func (f *fakeCloudAPI) ListDevices(context.Context) ([]domain.Device, error) {
	if f.devicesErr != nil {
		return nil, f.devicesErr
	}
	return f.devices, nil
}

func (f *fakeCloudAPI) Watching(_ context.Context, devicesCode, pwd string) error {
	f.watchingCalls++
	f.watchingCode, f.watchingPwd = devicesCode, pwd
	return f.watchingErr
}

func (f *fakeCloudAPI) GetA9VideoConf(_ context.Context, devID string) (domain.LiveConf, error) {
	f.confDevID = devID
	if f.confErr != nil {
		return domain.LiveConf{}, f.confErr
	}
	return f.conf, nil
}
