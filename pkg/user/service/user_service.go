package user_service

import (
	"context"
	"errors"
	"fmt"
	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	"github.com/lucasgiovannibr/whatygo/pkg/safemap"
	"sort"
	"time"

	instance_model "github.com/lucasgiovannibr/whatygo/pkg/instance/model"
	logger_wrapper "github.com/lucasgiovannibr/whatygo/pkg/logger"
	"github.com/lucasgiovannibr/whatygo/pkg/utils"
	whatsmeow_service "github.com/lucasgiovannibr/whatygo/pkg/whatsmeow/service"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// avatarRequestTimeout bounds POST /user/avatar so clients (e.g. Chatwoot at 12s)
// get a clear HTTP error instead of a hung connection waiting for the ~75s IQ default.
const avatarRequestTimeout = 8 * time.Second

// userInfoRequestTimeout bounds the usync query of POST /user/info.
const userInfoRequestTimeout = 10 * time.Second

// pictureURLEnrichBudget is the total wall-clock budget for the best-effort
// PictureURL lookups of one POST /user/info call (shared by all users in it).
const pictureURLEnrichBudget = 5 * time.Second

// clientReadyWait is the max time to wait after StartInstance before failing. The wait
// returns as soon as the client is connected; this is only the upper bound.
const clientReadyWait = utils.InstanceStartTimeout

type UserService interface {
	GetUser(ctx context.Context, data *CheckUserStruct, instance *instance_model.Instance) (*UserCollection, error)
	CheckUser(data *CheckUserStruct, instance *instance_model.Instance) (*CheckUserCollection, error)
	GetAvatar(ctx context.Context, data *GetAvatarStruct, instance *instance_model.Instance) (*types.ProfilePictureInfo, error)
	GetContacts(instance *instance_model.Instance) ([]ContactInfo, error)
	SaveContact(data *SaveContactStruct, instance *instance_model.Instance) error
	GetPrivacy(instance *instance_model.Instance) (types.PrivacySettings, error)
	SetPrivacy(data *PrivacyStruct, instance *instance_model.Instance) (*types.PrivacySettings, error)
	BlockContact(data *BlockStruct, instance *instance_model.Instance) (*types.Blocklist, error)
	UnlockContact(data *BlockStruct, instance *instance_model.Instance) (*types.Blocklist, error)
	GetBlockList(instance *instance_model.Instance) (*types.Blocklist, error)
	SetProfilePicture(data *SetProfilePictureStruct, instance *instance_model.Instance) (bool, error)
	SetProfileName(data *SetProfileNameStruct, instance *instance_model.Instance) (bool, error)
	SetProfileStatus(data *SetProfileStatusStruct, instance *instance_model.Instance) (bool, error)
	ResolveLid(data *ResolveLidStruct, instance *instance_model.Instance) (*ResolveLidResult, error)
	GetUserDevices(ctx context.Context, data *DevicesStruct, instance *instance_model.Instance) ([]UserDevice, error)
	GetStatusPrivacy(ctx context.Context, instance *instance_model.Instance) ([]StatusPrivacyEntry, error)
	GetBusinessProfile(ctx context.Context, data *BusinessProfileStruct, instance *instance_model.Instance) (*types.BusinessProfile, error)
}

type userService struct {
	clientPointer    *safemap.Map[*whatsmeow.Client]
	whatsmeowService whatsmeow_service.WhatsmeowService
	loggerWrapper    *logger_wrapper.LoggerManager
}

type ContactInfo struct {
	Jid          string `json:"Jid"`
	Found        bool   `json:"Found"`
	FirstName    string `json:"FirstName"`
	FullName     string `json:"FullName"`
	PushName     string `json:"PushName"`
	BusinessName string `json:"BusinessName"`
}

type UserInfo struct {
	VerifiedName *types.VerifiedName
	Status       string
	PictureID    string
	PictureURL   string
	Devices      []types.JID
	LID          *string // The local ID (if available)
}

type UserCollection struct {
	Users map[types.JID]UserInfo
}

type User struct {
	Query        string
	IsInWhatsapp bool
	JID          string
	RemoteJID    string
	LID          *string
	VerifiedName string
}

type CheckUserCollection struct {
	Users []User
}

type CheckUserStruct struct {
	Number    []string `json:"number"`
	FormatJid *bool    `json:"formatJid,omitempty"`
}

type GetAvatarStruct struct {
	Number  string `json:"number"`
	Preview bool   `json:"preview"`
}

type BlockStruct struct {
	Number string `json:"number"`
}

type SetProfilePictureStruct struct {
	Image string `json:"image"`
}

type SetProfileNameStruct struct {
	Name string `json:"name"`
}

type SetProfileStatusStruct struct {
	Status string `json:"status"`
}

type ResolveLidStruct struct {
	Lid      string `json:"lid"`
	GroupJid string `json:"groupJid,omitempty"`
}

type ResolveLidResult struct {
	Lid         string `json:"lid"`
	PhoneNumber string `json:"phoneNumber"`
	JID         string `json:"jid"`
}

type PrivacyStruct struct {
	GroupAdd     types.PrivacySetting `json:"groupAdd"`
	LastSeen     types.PrivacySetting `json:"lastSeen"`
	Status       types.PrivacySetting `json:"status"`
	Profile      types.PrivacySetting `json:"profile"`
	ReadReceipts types.PrivacySetting `json:"readReceipts"`
	CallAdd      types.PrivacySetting `json:"callAdd"`
	Online       types.PrivacySetting `json:"online"`
}

func (u *userService) ensureClientConnected(instanceId string) (*whatsmeow.Client, error) {
	return u.ensureClientConnectedCtx(context.Background(), instanceId)
}

func (u *userService) ensureClientConnectedCtx(ctx context.Context, instanceId string) (*whatsmeow.Client, error) {
	return utils.ClientProvider{Clients: u.clientPointer, Starter: u.whatsmeowService, Gate: true, Wait: clientReadyWait}.Ensure(ctx, instanceId, u.loggerWrapper.GetLogger(instanceId))
}

func (u *userService) GetUser(ctx context.Context, data *CheckUserStruct, instance *instance_model.Instance) (*UserCollection, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	client, err := u.ensureClientConnectedCtx(ctx, instance.Id)
	if err != nil {
		return nil, err
	}

	var jids []types.JID
	for _, arg := range data.Number {
		jid, ok := utils.ParseJID(arg)
		if !ok {
			return nil, apierror.Invalid("invalid phone number")
		}
		// The usync query is a RAW IQ: the "+" that CreateJID adds makes WhatsApp
		// ignore it and the request only ends at the timeout (see utils.CanonicalJID).
		jid = utils.CanonicalJID(jid).ToNonAD()
		// Prefer the phone-number JID when the store knows the mapping for a @lid.
		if jid.Server == types.HiddenUserServer && client.Store.LIDs != nil {
			if pn, lidErr := client.Store.LIDs.GetPNForLID(ctx, jid); lidErr == nil && !pn.IsEmpty() {
				jid = utils.CanonicalJID(pn).ToNonAD()
			}
		}
		jids = append(jids, jid)
	}

	usyncCtx, cancel := context.WithTimeout(ctx, userInfoRequestTimeout)
	defer cancel()
	resp, err := client.GetUserInfo(usyncCtx, jids)
	if err != nil {
		return nil, err
	}

	uc := new(UserCollection)
	uc.Users = make(map[types.JID]UserInfo)

	enrichDeadline := time.Now().Add(pictureURLEnrichBudget)
	skipPictureEnrich := false

	for jid, whatsmeowInfo := range resp {
		// Consultar LID Store para obter LID associado ao JID
		var lidStr *string
		if client.Store.LIDs != nil {
			if lid, err := client.Store.LIDs.GetLIDForPN(ctx, jid); err == nil && !lid.IsEmpty() {
				lidString := fmt.Sprintf("%v", lid)
				lidStr = &lidString
			}
		}

		// Best-effort picture URL: PictureID alone is not usable by consumers. It
		// shares one time budget across all users and stops on WhatsApp rate limits,
		// so a large batch can never make /user/info hang.
		pictureURL := ""
		if !skipPictureEnrich && whatsmeowInfo.PictureID != "" {
			remaining := time.Until(enrichDeadline)
			if remaining <= 0 {
				skipPictureEnrich = true
			} else {
				picCtx, picCancel := context.WithTimeout(ctx, remaining)
				pic, picErr := client.GetProfilePictureInfo(picCtx, utils.CanonicalJID(jid).ToNonAD(), &whatsmeow.GetProfilePictureParams{Preview: true})
				picCancel()
				if picErr != nil {
					u.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] Failed to enrich PictureURL for %s: %v", instance.Id, jid, picErr)
					if errors.Is(picErr, whatsmeow.ErrIQRateOverLimit) {
						skipPictureEnrich = true
					}
				} else if pic != nil {
					pictureURL = pic.URL
				}
			}
		}

		// Converter para nossa estrutura UserInfo que inclui LID
		info := UserInfo{
			VerifiedName: whatsmeowInfo.VerifiedName,
			Status:       whatsmeowInfo.Status,
			PictureID:    whatsmeowInfo.PictureID,
			PictureURL:   pictureURL,
			Devices:      whatsmeowInfo.Devices,
			LID:          lidStr,
		}
		uc.Users[jid] = info
	}

	return uc, nil
}

func (u *userService) CheckUser(data *CheckUserStruct, instance *instance_model.Instance) (*CheckUserCollection, error) {
	client, err := u.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	// Set formatJid to false by default for CheckUser
	formatJid := false
	if data.FormatJid != nil {
		formatJid = *data.FormatJid
	}

	// First attempt with the requested formatJid setting
	uc, shouldRetry, err := u.performCheckUser(client, data.Number, formatJid, instance.Id)
	if err != nil {
		// A failed check used to come back as 200 with `data: null`, indistinguishable
		// from "checked, nothing to report".
		return nil, err
	}
	if !shouldRetry {
		return uc, nil
	}

	// If formatJid was true and we got false results, retry with formatJid=false
	if formatJid {
		u.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Some users not found with formatJid=true, retrying with formatJid=false", instance.Id)
		ucRetry, _, _ := u.performCheckUser(client, data.Number, false, instance.Id)

		// Merge results: use retry results for users that weren't found in first attempt
		return u.mergeCheckUserResults(uc, ucRetry), nil
	}

	return uc, nil
}

// performCheckUser executes the actual user check with specified formatJid
func (u *userService) performCheckUser(client *whatsmeow.Client, numbers []string, formatJid bool, instanceId string) (*CheckUserCollection, bool, error) {
	// Use centralized function to prepare numbers for WhatsApp check
	phoneNumbers, err := utils.PrepareNumbersForWhatsAppCheck(numbers, &formatJid)
	if err != nil {
		u.loggerWrapper.GetLogger(instanceId).LogWarn("[%s] Failed to prepare numbers for WhatsApp check: %v", instanceId, err)
		return nil, false, &InvalidNumberError{Err: err}
	}

	resp, err := client.IsOnWhatsApp(context.Background(), phoneNumbers)
	if err != nil {
		u.loggerWrapper.GetLogger(instanceId).LogError("[%s] Failed to check users on WhatsApp: %v", instanceId, err)
		return nil, false, fmt.Errorf("failed to check users on WhatsApp: %w", err)
	}

	uc := new(CheckUserCollection)
	shouldRetry := false

	for _, item := range resp {
		// Consultar LID Store para obter LID associado ao JID
		var lidStr *string
		if client.Store.LIDs != nil {
			if lid, err := client.Store.LIDs.GetLIDForPN(context.TODO(), item.JID); err == nil && !lid.IsEmpty() {
				lidString := fmt.Sprintf("%v", lid)
				lidStr = &lidString
			}
		}

		// Determine the RemoteJID to use for messaging
		remoteJID := item.Query // Default to original query
		if item.IsIn {
			// When user exists on WhatsApp, use the JID returned by WhatsApp
			remoteJID = fmt.Sprintf("%v", item.JID)
		} else if formatJid {
			// If user not found and we used formatJid=true, we should retry with formatJid=false
			shouldRetry = true
		}

		if item.VerifiedName != nil {
			var msg = User{
				Query:        item.Query,
				IsInWhatsapp: item.IsIn,
				JID:          fmt.Sprintf("%v", item.JID),
				RemoteJID:    remoteJID,
				LID:          lidStr,
				VerifiedName: item.VerifiedName.Details.GetVerifiedName(),
			}
			uc.Users = append(uc.Users, msg)
		} else {
			var msg = User{
				Query:        item.Query,
				IsInWhatsapp: item.IsIn,
				JID:          fmt.Sprintf("%v", item.JID),
				RemoteJID:    remoteJID,
				LID:          lidStr,
				VerifiedName: "",
			}
			uc.Users = append(uc.Users, msg)
		}
	}

	return uc, shouldRetry, nil
}

// InvalidNumberError is a number the caller sent that cannot be checked.
type InvalidNumberError struct{ Err error }

func (e *InvalidNumberError) Error() string { return e.Err.Error() }
func (e *InvalidNumberError) Unwrap() error { return e.Err }

// mergeCheckUserResults merges results from two CheckUser attempts
// Priority: if a user is found in retry (formatJid=false), use that result
func (u *userService) mergeCheckUserResults(original, retry *CheckUserCollection) *CheckUserCollection {
	if retry == nil {
		return original
	}

	// Create a map of retry results by original query for quick lookup
	retryMap := make(map[string]User)
	for _, user := range retry.Users {
		retryMap[user.Query] = user
	}

	// Merge results
	merged := &CheckUserCollection{}
	for _, originalUser := range original.Users {
		if retryUser, exists := retryMap[originalUser.Query]; exists && retryUser.IsInWhatsapp && !originalUser.IsInWhatsapp {
			// Use retry result if it found the user and original didn't
			merged.Users = append(merged.Users, retryUser)
		} else {
			// Use original result
			merged.Users = append(merged.Users, originalUser)
		}
	}

	return merged
}

// ResolveLid resolves the phone number mapped to a LID (Linked Identity, "xxxx@lid").
// This only reads whatsmeow's local LID store: the WhatsApp protocol has no
// server query for LID->PN (only the inverse, PN->LID, is supported). The
// mapping is only known locally after it has been received passively (e.g.
// a message from that LID, or a group with LID-based participants). As a
// best-effort fallback, when a groupJid is provided and the mapping is
// missing, a fresh GetGroupInfo on that group can populate it, since group
// participant lists include the phone number for LID participants.
func (u *userService) ResolveLid(data *ResolveLidStruct, instance *instance_model.Instance) (*ResolveLidResult, error) {
	client, err := u.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	lidJID, ok := utils.ParseJID(data.Lid)
	if !ok || lidJID.Server != types.HiddenUserServer {
		return nil, apierror.Invalid("invalid lid")
	}

	if client.Store.LIDs == nil {
		return nil, errors.New("lid store unavailable")
	}

	ctx := context.Background()

	pn, err := client.Store.LIDs.GetPNForLID(ctx, lidJID)
	if err != nil {
		return nil, err
	}

	if pn.IsEmpty() && data.GroupJid != "" {
		groupJID, ok := utils.ParseJID(data.GroupJid)
		if !ok || groupJID.Server != types.GroupServer {
			return nil, apierror.Invalid("invalid groupJid")
		}

		u.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] No cached phone number for lid %s, refreshing group %s as fallback", instance.Id, lidJID, groupJID)

		if _, groupErr := client.GetGroupInfo(ctx, groupJID); groupErr != nil {
			u.loggerWrapper.GetLogger(instance.Id).LogWarn("[%s] Failed to refresh group %s for lid fallback: %v", instance.Id, groupJID, groupErr)
		} else if pn, err = client.Store.LIDs.GetPNForLID(ctx, lidJID); err != nil {
			return nil, err
		}
	}

	if pn.IsEmpty() {
		return nil, errors.New("no phone number mapping found for this lid")
	}

	return &ResolveLidResult{
		Lid:         lidJID.String(),
		PhoneNumber: pn.User,
		JID:         pn.String(),
	}, nil
}

func (u *userService) GetAvatar(ctx context.Context, data *GetAvatarStruct, instance *instance_model.Instance) (*types.ProfilePictureInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	client, err := u.ensureClientConnectedCtx(ctx, instance.Id)
	if err != nil {
		return nil, err
	}

	// 🔒 FIX: Verificar se o cliente está conectado antes de fazer a requisição
	if !client.IsConnected() {
		return nil, errors.New("client is not connected to WhatsApp")
	}

	// 🔒 FIX: Verificar se o cliente está autenticado
	if !client.IsLoggedIn() {
		return nil, errors.New("client is not logged in to WhatsApp")
	}

	jid, ok := utils.ParseJID(data.Number)
	if !ok {
		return nil, apierror.Invalid("invalid phone number")
	}
	// Profile picture IQ is a RAW node (Target=jid). CreateJID/ParseJID may
	// prefix "+" which WhatsApp does not accept on this path — same class of
	// bug as typing/receipts (see utils.CanonicalJID).
	jid = utils.CanonicalJID(jid).ToNonAD()
	// Prefer PN JID when the store knows the mapping for @lid.
	if jid.Server == types.HiddenUserServer && client.Store.LIDs != nil {
		if pn, lidErr := client.Store.LIDs.GetPNForLID(ctx, jid); lidErr == nil && !pn.IsEmpty() {
			u.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Resolved LID %s to PN %s for avatar", instance.Id, jid, pn)
			jid = utils.CanonicalJID(pn).ToNonAD()
		}
	}

	u.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Requesting avatar for JID: %s, Preview: %v", instance.Id, jid, data.Preview)

	reqCtx, cancel := context.WithTimeout(ctx, avatarRequestTimeout)
	defer cancel()

	u.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Starting GetProfilePictureInfo request...", instance.Id)
	pic, err := client.GetProfilePictureInfo(reqCtx, jid, &whatsmeow.GetProfilePictureParams{
		Preview: data.Preview,
	})
	if err != nil {
		u.loggerWrapper.GetLogger(instance.Id).LogError("[%s] GetProfilePictureInfo failed: %v", instance.Id, err)
		return nil, fmt.Errorf("get profile picture for %s: %w", jid, err)
	}

	if pic == nil {
		return nil, errors.New("no profile picture found")
	}

	u.loggerWrapper.GetLogger(instance.Id).LogInfo("[%s] Got avatar %s", instance.Id, pic.URL)

	return pic, nil
}

func (u *userService) GetContacts(instance *instance_model.Instance) ([]ContactInfo, error) {
	client, err := u.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	contacts, err := client.Store.Contacts.GetAllContacts(context.Background())
	if err != nil {
		return nil, err
	}

	// Never null (an account without contacts answered `data: null`), and in a stable
	// order: the store returns a map, so every call listed them differently.
	contactsArray := make([]ContactInfo, 0, len(contacts))

	for jid, contact := range contacts {
		contactsArray = append(contactsArray, ContactInfo{
			Jid:          jid.String(),
			Found:        contact.Found,
			FirstName:    contact.FirstName,
			FullName:     contact.FullName,
			PushName:     contact.PushName,
			BusinessName: contact.BusinessName,
		})
	}

	sort.Slice(contactsArray, func(i, j int) bool { return contactsArray[i].Jid < contactsArray[j].Jid })

	return contactsArray, nil
}

func (u *userService) GetPrivacy(instance *instance_model.Instance) (types.PrivacySettings, error) {
	client, err := u.ensureClientConnected(instance.Id)
	if err != nil {
		return types.PrivacySettings{}, err
	}

	privacy := client.GetPrivacySettings(context.Background())

	return privacy, nil
}

func (u *userService) SetPrivacy(data *PrivacyStruct, instance *instance_model.Instance) (*types.PrivacySettings, error) {
	client, err := u.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	privacySettings := []struct {
		name  types.PrivacySettingType
		value types.PrivacySetting
	}{
		{types.PrivacySettingTypeGroupAdd, data.GroupAdd},
		{types.PrivacySettingTypeLastSeen, data.LastSeen},
		{types.PrivacySettingTypeStatus, data.Status},
		{types.PrivacySettingTypeProfile, data.Profile},
		{types.PrivacySettingTypeReadReceipts, data.ReadReceipts},
		{types.PrivacySettingTypeCallAdd, data.CallAdd},
		{types.PrivacySettingTypeOnline, data.Online},
	}

	for _, setting := range privacySettings {
		_, err := client.SetPrivacySetting(context.Background(), setting.name, setting.value)
		if err != nil {
			return nil, err
		}
	}

	privacy := client.GetPrivacySettings(context.Background())

	return &privacy, nil
}

func (u *userService) BlockContact(data *BlockStruct, instance *instance_model.Instance) (*types.Blocklist, error) {
	client, err := u.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	jid, ok := utils.ParseJID(data.Number)
	if !ok {
		return nil, apierror.Invalid("invalid phone number")
	}
	// UpdateBlocklist resolves the LID of a phone number through the store and, when it
	// is unknown, through a usync query. With the "+" that CreateJID adds, neither
	// finds the user: the query waited for its timeout and the call failed (see
	// utils.CanonicalJID).
	jid = utils.CanonicalJID(jid)

	resp, err := client.UpdateBlocklist(context.Background(), jid, events.BlocklistChangeActionBlock)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (u *userService) UnlockContact(data *BlockStruct, instance *instance_model.Instance) (*types.Blocklist, error) {
	client, err := u.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	jid, ok := utils.ParseJID(data.Number)
	if !ok {
		return nil, apierror.Invalid("invalid phone number")
	}
	// UpdateBlocklist resolves the LID of a phone number through the store and, when it
	// is unknown, through a usync query. With the "+" that CreateJID adds, neither
	// finds the user: the query waited for its timeout and the call failed (see
	// utils.CanonicalJID).
	jid = utils.CanonicalJID(jid)

	resp, err := client.UpdateBlocklist(context.Background(), jid, events.BlocklistChangeActionUnblock)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (u *userService) GetBlockList(instance *instance_model.Instance) (*types.Blocklist, error) {
	client, err := u.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}

	resp, err := client.GetBlocklist(context.Background())
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (u *userService) SetProfilePicture(data *SetProfilePictureStruct, instance *instance_model.Instance) (bool, error) {
	client, err := u.ensureClientConnected(instance.Id)
	if err != nil {
		return false, err
	}

	filedata, err := utils.DownloadBytes(data.Image, utils.MaxImageDownload)
	if err != nil {
		return false, fmt.Errorf("failed to fetch image from URL: %v", err)
	}

	_, err = client.SetGroupPhoto(context.Background(), types.EmptyJID, filedata)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (u *userService) SetProfileName(data *SetProfileNameStruct, instance *instance_model.Instance) (bool, error) {
	client, err := u.ensureClientConnected(instance.Id)
	if err != nil {
		return false, err
	}

	// The account's own display name (push name) is an app-state setting. It used
	// to call SetGroupName with an empty JID, which sends a group IQ to nobody and
	// never gets an answer, so the request hung until the client gave up (#176).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = client.SendAppState(ctx, appstate.BuildSettingPushName(data.Name))
	if err != nil {
		return false, err
	}

	return true, nil
}

func (u *userService) SetProfileStatus(data *SetProfileStatusStruct, instance *instance_model.Instance) (bool, error) {
	client, err := u.ensureClientConnected(instance.Id)
	if err != nil {
		return false, err
	}

	err = client.SetStatusMessage(context.Background(), types.SetStatusInput{Text: &data.Status})
	if err != nil {
		return false, err
	}

	return true, nil
}

func NewUserService(
	clientPointer *safemap.Map[*whatsmeow.Client],
	whatsmeowService whatsmeow_service.WhatsmeowService,
	loggerWrapper *logger_wrapper.LoggerManager,
) UserService {
	return &userService{
		clientPointer:    clientPointer,
		whatsmeowService: whatsmeowService,
		loggerWrapper:    loggerWrapper,
	}
}
