package instance_model

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Instance struct {
	Id               string    `json:"id" gorm:"type:uuid;primaryKey"`
	Name             string    `json:"name"`
	Token            string    `json:"token" gorm:"unique"`
	Webhook          string    `json:"webhook"`
	RabbitmqEnable   string    `json:"rabbitmqEnable"`
	WebSocketEnable  string    `json:"websocketEnable"`
	NatsEnable       string    `json:"natsEnable"`
	Jid              string    `json:"jid" gorm:"column:jid"`
	Qrcode           string    `json:"qrcode" gorm:"type:text"`
	Connected        bool      `json:"connected"`
	Expiration       int64     `json:"expiration"`
	DisconnectReason string    `json:"disconnect_reason"`
	Events           string    `json:"events"`
	OsName           string    `json:"os_name"`
	Proxy            string    `json:"proxy"`
	ClientName       string    `json:"client_name" gorm:"index"`
	CreatedAt        time.Time `json:"createdAt" gorm:"autoCreateTime"`

	// Advanced Settings
	AlwaysOnline  bool   `json:"alwaysOnline" gorm:"default:false"`
	RejectCall    bool   `json:"rejectCall" gorm:"default:false"`
	MsgRejectCall string `json:"msgRejectCall" gorm:"default:''"`
	ReadMessages  bool   `json:"readMessages" gorm:"default:false"`
	IgnoreGroups  bool   `json:"ignoreGroups" gorm:"default:false"`
	IgnoreStatus  bool   `json:"ignoreStatus" gorm:"default:false"`
	// CallsEnabled gives the instance a WhatsApp call engine (experimental). It is
	// read when the client starts, so changing it takes effect on the next connection.
	CallsEnabled bool `json:"callsEnabled" gorm:"default:false"`
}

// redactProxy removes the password from the proxy JSON stored on an instance. The API
// returned the whole record, so creating or listing instances handed the proxy credentials
// to every caller (and to every log or UI that kept the response).
func redactProxy(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return raw
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return "" // not something we can vouch for: do not echo it
	}
	delete(cfg, "password")
	out, err := json.Marshal(cfg)
	if err != nil {
		return ""
	}
	return string(out)
}

// MarshalJSON keeps the proxy password out of every response that serializes an Instance.
// The database column (gorm) is not affected, so connecting still uses the real value.
func (i Instance) MarshalJSON() ([]byte, error) {
	type plain Instance // no methods: avoids recursing into MarshalJSON
	p := plain(i)
	p.Proxy = redactProxy(p.Proxy)
	return json.Marshal(p)
}

// AdvancedSettings representa as configurações avançadas de uma instância.
// Bool fields are pointers so omitted JSON keys are not written as false on PUT.
type AdvancedSettings struct {
	AlwaysOnline *bool `json:"alwaysOnline"`
	RejectCall   *bool `json:"rejectCall"`
	// MsgRejectCall is a pointer so that a PUT can tell "not sent" (nil, left alone) from ""
	// (clear the message).
	MsgRejectCall *string `json:"msgRejectCall"`
	ReadMessages  *bool   `json:"readMessages"`
	IgnoreGroups  *bool   `json:"ignoreGroups"`
	IgnoreStatus  *bool   `json:"ignoreStatus"`
	// CallsEnabled: see Instance.CallsEnabled. Takes effect on the next connection.
	CallsEnabled *bool `json:"callsEnabled"`
}

func (m *Instance) BeforeCreate(tx *gorm.DB) (err error) {
	if m.Id == "" {
		m.Id = uuid.New().String()
	}
	return
}

// BoolPtr returns a pointer to v (helper for AdvancedSettings responses/tests).
func BoolPtr(v bool) *bool {
	return &v
}
