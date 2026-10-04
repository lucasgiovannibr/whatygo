package whatsmeow_service

import (
	"context"
	"errors"
	"fmt"

	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
	"go.mau.fi/whatsmeow/store/sqlstore"
)

// PurgeInstanceData removes what a deleted instance leaves in the auth database: the
// paired device with everything hanging off it (session and identity keys, sender keys,
// app-state keys, thousands of cached contacts, the LID map...) and its poll votes.
//
// Deleting an instance only logged the device out when the client was connected. An
// instance that was paired but not connected at that moment (the usual case for one that
// was disconnected or that lost its websocket) kept all of it in the database for good.
// The phone still lists the session as a linked device in that case: the user removes it
// there, since only a connected client can log itself out.
func (w whatsmeowService) PurgeInstanceData(instanceID, jid string) error {
	var errs []error

	if jid != "" {
		container, err := w.getAuthContainer()
		if err != nil {
			errs = append(errs, fmt.Errorf("auth container: %w", err))
		} else if err := purgeDevice(context.Background(), container, jid); err != nil {
			errs = append(errs, err)
		}
	}

	if w.pollService != nil {
		if err := w.pollService.DeleteInstanceVotes(context.Background(), instanceID); err != nil {
			errs = append(errs, fmt.Errorf("poll votes: %w", err))
		}
	}

	// The media this instance stored in the object storage.
	if w.mediaStorage != nil {
		if _, err := w.mediaStorage.DeleteInstance(context.Background(), instanceID); err != nil {
			errs = append(errs, fmt.Errorf("stored media: %w", err))
		}
	}

	return errors.Join(errs...)
}

// purgeDevice deletes the stored device of a paired JID, if there still is one.
func purgeDevice(ctx context.Context, container *sqlstore.Container, jid string) error {
	parsed, ok := utils.ParseJID(jid)
	if !ok {
		return apierror.Invalid(fmt.Sprintf("invalid jid %q", jid))
	}
	device, err := container.GetDevice(ctx, parsed)
	if err != nil {
		return fmt.Errorf("get device: %w", err)
	}
	if device == nil {
		return nil // already removed, e.g. by a successful logout
	}
	if err := device.Delete(ctx); err != nil {
		return fmt.Errorf("delete device: %w", err)
	}
	return nil
}
