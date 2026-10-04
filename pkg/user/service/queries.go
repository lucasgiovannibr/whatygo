package user_service

import (
	"context"
	"errors"
	"strings"
	"time"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// queryTimeout bounds the read-only lookups below so a request never hangs on the IQ
// default of ~75 s.
const queryTimeout = 10 * time.Second

// maxDeviceQueryNumbers bounds POST /user/devices.
const maxDeviceQueryNumbers = 50

// NotFoundError is a lookup that legitimately has no result (a number that is not a
// business account).
type NotFoundError struct{ Msg string }

func (e *NotFoundError) Error() string { return e.Msg }

// DevicesStruct is the body of POST /user/devices.
type DevicesStruct struct {
	// Number is one number or a list of numbers.
	Number []string `json:"number"`
}

// UserDevice is one linked device of a user. Device 0 is the primary phone.
type UserDevice struct {
	JID    string `json:"jid"`
	User   string `json:"user"`
	Device uint16 `json:"device"`
	Server string `json:"server"`
}

// BusinessProfileStruct is the body of POST /user/business.
type BusinessProfileStruct struct {
	Number string `json:"number"`
}

// StatusPrivacyEntry is one stored "who sees my status" setting.
type StatusPrivacyEntry struct {
	Type      string   `json:"type"`
	List      []string `json:"list"`
	IsDefault bool     `json:"isDefault"`
}

// GetUserDevices lists the linked devices of one or several users (the phone is device
// 0). WhatsApp does not include this account's own device in the answer.
func (u *userService) GetUserDevices(ctx context.Context, data *DevicesStruct, instance *instance_model.Instance) ([]UserDevice, error) {
	if len(data.Number) == 0 {
		return nil, &InvalidNumberError{Err: errors.New("phone number is required")}
	}
	if len(data.Number) > maxDeviceQueryNumbers {
		return nil, &InvalidNumberError{Err: errors.New("too many numbers: at most 50 per request")}
	}

	jids := make([]types.JID, 0, len(data.Number))
	for _, n := range data.Number {
		parsed, ok := utils.ParseJID(n)
		if !ok {
			return nil, &InvalidNumberError{Err: errors.New("invalid phone number: " + n)}
		}
		jids = append(jids, utils.CanonicalJID(parsed).ToNonAD())
	}

	client, err := u.ensureClientConnectedCtx(ctx, instance.Id)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	devices, err := client.GetUserDevices(ctx, jids)
	if err != nil {
		return nil, err
	}

	out := make([]UserDevice, 0, len(devices))
	for _, d := range devices {
		out = append(out, UserDevice{JID: d.String(), User: d.User, Device: d.Device, Server: d.Server})
	}
	return out, nil
}

// GetStatusPrivacy returns the stored "who sees my status" settings; the first one is
// the default.
func (u *userService) GetStatusPrivacy(ctx context.Context, instance *instance_model.Instance) ([]StatusPrivacyEntry, error) {
	client, err := u.ensureClientConnectedCtx(ctx, instance.Id)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	settings, err := client.GetStatusPrivacy(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]StatusPrivacyEntry, 0, len(settings))
	for _, s := range settings {
		list := make([]string, 0, len(s.List))
		for _, j := range s.List {
			list = append(list, j.String())
		}
		out = append(out, StatusPrivacyEntry{Type: string(s.Type), List: list, IsDefault: s.IsDefault})
	}
	return out, nil
}

// isNoBusinessProfile recognises the answer for an account that is not a business. The
// library has no sentinel for it: an ordinary account answers with an empty profile node
// and the parser fails with a plain "missing jid in business profile" error.
func isNoBusinessProfile(err error) bool {
	var missing *whatsmeow.ElementMissingError
	return errors.Is(err, whatsmeow.ErrIQNotFound) || errors.As(err, &missing) ||
		strings.Contains(err.Error(), "missing jid in business profile")
}

// GetBusinessProfile returns the public profile of a WhatsApp Business account. An
// account that is not a business answers *NotFoundError.
func (u *userService) GetBusinessProfile(ctx context.Context, data *BusinessProfileStruct, instance *instance_model.Instance) (*types.BusinessProfile, error) {
	if data.Number == "" {
		return nil, &InvalidNumberError{Err: errors.New("phone number is required")}
	}
	parsed, ok := utils.ParseJID(data.Number)
	if !ok {
		return nil, &InvalidNumberError{Err: errors.New("invalid phone number")}
	}
	jid := utils.CanonicalJID(parsed).ToNonAD()

	client, err := u.ensureClientConnectedCtx(ctx, instance.Id)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	profile, err := client.GetBusinessProfile(ctx, jid)
	if err != nil {
		if isNoBusinessProfile(err) {
			return nil, &NotFoundError{Msg: "this number has no business profile"}
		}
		return nil, err
	}
	if profile == nil {
		return nil, &NotFoundError{Msg: "this number has no business profile"}
	}
	return profile, nil
}
