package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/lucasgiovannibr/whatygo/docs"
	call_handler "github.com/lucasgiovannibr/whatygo/pkg/call/handler"
	chat_handler "github.com/lucasgiovannibr/whatygo/pkg/chat/handler"
	community_handler "github.com/lucasgiovannibr/whatygo/pkg/community/handler"
	group_handler "github.com/lucasgiovannibr/whatygo/pkg/group/handler"
	instance_handler "github.com/lucasgiovannibr/whatygo/pkg/instance/handler"
	label_handler "github.com/lucasgiovannibr/whatygo/pkg/label/handler"
	message_handler "github.com/lucasgiovannibr/whatygo/pkg/message/handler"
	auth_middleware "github.com/lucasgiovannibr/whatygo/pkg/middleware"
	newsletter_handler "github.com/lucasgiovannibr/whatygo/pkg/newsletter/handler"
	poll_handler "github.com/lucasgiovannibr/whatygo/pkg/poll/handler"
	send_handler "github.com/lucasgiovannibr/whatygo/pkg/sendMessage/handler"
	server_handler "github.com/lucasgiovannibr/whatygo/pkg/server/handler"
	user_handler "github.com/lucasgiovannibr/whatygo/pkg/user/handler"
)

type Routes struct {
	authMiddleware          auth_middleware.Middleware
	jidValidationMiddleware *auth_middleware.JIDValidationMiddleware
	instanceHandler         instance_handler.InstanceHandler
	userHandler             user_handler.UserHandler
	sendHandler             send_handler.SendHandler
	messageHandler          message_handler.MessageHandler
	chatHandler             chat_handler.ChatHandler
	groupHandler            group_handler.GroupHandler
	callHandler             call_handler.CallHandler
	communityHandler        community_handler.CommunityHandler
	labelHandler            label_handler.LabelHandler
	newsletterHandler       newsletter_handler.NewsletterHandler
	pollHandler             *poll_handler.PollHandler
	serverHandler           server_handler.ServerHandler
}

func (r *Routes) AssignRoutes(eng *gin.Engine) {
	eng.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	eng.GET("/favicon.ico", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	// Rotas para o gerenciador React (sem autenticação)
	// Browser-side protections (CSP, no framing, no sniffing) for the manager pages only.
	managerHeaders := auth_middleware.ManagerSecurityHeaders()
	eng.Group("/assets", managerHeaders).Static("/", "./manager/dist/assets")

	// Ajuste nas rotas do manager para suportar client-side routing do React
	eng.GET("/manager/*any", managerHeaders, func(c *gin.Context) {
		c.File("manager/dist/index.html")
	})

	eng.GET("/manager", managerHeaders, func(c *gin.Context) {
		c.File("manager/dist/index.html")
	})

	eng.GET("/server/ok", r.serverHandler.ServerOk)
	eng.GET("/health", r.serverHandler.Health) // readiness: databases + pool saturation

	routes := eng.Group("/instance")
	{
		routes.Use(r.authMiddleware.AuthAdmin)
		{
			routes.POST("/create", r.instanceHandler.Create)
			routes.GET("/all", r.instanceHandler.All)
			routes.GET("/info/:instanceId", r.instanceHandler.Info)
			routes.DELETE("/delete/:instanceId", r.instanceHandler.Delete)
			routes.POST("/proxy/:instanceId", r.instanceHandler.SetProxy)
			routes.DELETE("/proxy/:instanceId", r.instanceHandler.DeleteProxy)
			routes.GET("/proxy/:instanceId", r.instanceHandler.GetProxyStatus)
			routes.GET("/runtimes", r.instanceHandler.GetRuntimes)
			routes.POST("/forcereconnect/:instanceId", r.instanceHandler.ForceReconnect)
			routes.GET("/logs/:instanceId", r.instanceHandler.GetLogs)
		}
	}

	routes = eng.Group("/instance")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/connect", r.instanceHandler.Connect)
			routes.GET("/status", r.instanceHandler.Status)
			routes.GET("/qr", r.instanceHandler.Qr)
			routes.POST("/pair", r.jidValidationMiddleware.ValidateNumberField(), r.instanceHandler.Pair)
			routes.POST("/disconnect", r.instanceHandler.Disconnect)
			routes.POST("/reconnect", r.instanceHandler.Reconnect)
			routes.DELETE("/logout", r.instanceHandler.Logout)
		}
	}

	// Advanced settings: global API key, or the token of that same instance (#81).
	routes = eng.Group("/instance")
	{
		routes.Use(r.authMiddleware.AuthInstanceScoped)
		{
			routes.GET("/:instanceId/advanced-settings", r.instanceHandler.GetAdvancedSettings)
			routes.PUT("/:instanceId/advanced-settings", r.instanceHandler.UpdateAdvancedSettings)
			routes.PUT("/:instanceId/integrations", r.instanceHandler.UpdateIntegrations)
			routes.GET("/:instanceId/runtime", r.instanceHandler.GetRuntime)
		}
	}

	routes = eng.Group("/send")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/text", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendText)
			routes.POST("/link", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendLink)
			routes.POST("/media", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendMedia)
			routes.POST("/poll", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendPoll)
			routes.POST("/pollVote", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendPollVote)
			routes.POST("/sticker", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendSticker)
			routes.POST("/location", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendLocation)
			routes.POST("/contact", r.jidValidationMiddleware.ValidateContactFields(), r.sendHandler.SendContact) // TODO: send multiple contacts
			routes.POST("/button", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendButton)
			routes.POST("/list", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendList)
			routes.POST("/carousel", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.sendHandler.SendCarousel)
			routes.POST("/status/text", r.sendHandler.SendStatusText)
			routes.POST("/status/media", r.sendHandler.SendStatusMedia)
		}
	}
	routes = eng.Group("/user")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/info", r.jidValidationMiddleware.ValidateNumberField(), r.userHandler.GetUser)
			routes.POST("/check", r.jidValidationMiddleware.ValidateNumberFieldWithFormatJid(), r.userHandler.CheckUser)
			routes.POST("/avatar", r.jidValidationMiddleware.ValidateNumberField(), r.userHandler.GetAvatar)
			routes.GET("/contacts", r.userHandler.GetContacts)
			routes.POST("/contacts", r.userHandler.SaveContact)
			routes.GET("/privacy", r.userHandler.GetPrivacy)
			routes.POST("/privacy", r.userHandler.SetPrivacy)
			routes.POST("/block", r.jidValidationMiddleware.ValidateNumberField(), r.userHandler.BlockContact)
			routes.POST("/unblock", r.jidValidationMiddleware.ValidateNumberField(), r.userHandler.UnblockContact)
			routes.GET("/blocklist", r.userHandler.GetBlockList)
			routes.POST("/profilePicture", r.userHandler.SetProfilePicture)
			routes.POST("/profileName", r.userHandler.SetProfileName)
			routes.POST("/profileStatus", r.userHandler.SetProfileStatus)
			routes.POST("/lid", r.jidValidationMiddleware.ValidateJIDFields("lid", "groupJid"), r.userHandler.ResolveLid)
			routes.POST("/devices", r.userHandler.GetUserDevices)
			routes.GET("/statusprivacy", r.userHandler.GetStatusPrivacy)
			routes.POST("/business", r.userHandler.GetBusinessProfile)
			routes.POST("/defaultDisappearing", r.chatHandler.SetDefaultDisappearing)
		}
	}
	routes = eng.Group("/message")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/react", r.jidValidationMiddleware.ValidateJIDFields("number"), r.messageHandler.React)
			routes.POST("/presence", r.jidValidationMiddleware.ValidateNumberField(), r.messageHandler.ChatPresence)
			routes.POST("/subscribe", r.jidValidationMiddleware.ValidateNumberField(), r.messageHandler.SubscribePresence)
			routes.POST("/markread", r.jidValidationMiddleware.ValidateNumberField(), r.messageHandler.MarkRead)
			routes.POST("/markplayed", r.jidValidationMiddleware.ValidateNumberField(), r.messageHandler.MarkPlayed)
			routes.POST("/downloadmedia", r.messageHandler.DownloadMedia)
			routes.POST("/status", r.messageHandler.GetMessageStatus)
			routes.POST("/delete", r.jidValidationMiddleware.ValidateNumberField(), r.messageHandler.DeleteMessageEveryone)
			routes.POST("/edit", r.jidValidationMiddleware.ValidateNumberField(), r.messageHandler.EditMessage) // TODO: edit MediaMessage too
			routes.POST("/rerequest", r.messageHandler.RerequestMessage)
		}
	}
	routes = eng.Group("/chat")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/pin", r.jidValidationMiddleware.ValidateNumberField(), r.chatHandler.ChatPin)
			routes.POST("/unpin", r.jidValidationMiddleware.ValidateNumberField(), r.chatHandler.ChatUnpin)
			routes.POST("/archive", r.jidValidationMiddleware.ValidateNumberField(), r.chatHandler.ChatArchive)
			routes.POST("/unarchive", r.jidValidationMiddleware.ValidateNumberField(), r.chatHandler.ChatUnarchive)
			routes.POST("/mute", r.jidValidationMiddleware.ValidateNumberField(), r.chatHandler.ChatMute)
			routes.POST("/unmute", r.jidValidationMiddleware.ValidateNumberField(), r.chatHandler.ChatUnmute)
			routes.POST("/history-sync", r.chatHandler.HistorySyncRequest)
			routes.POST("/disappearing", r.chatHandler.SetDisappearing)
		}
	}
	routes = eng.Group("/group")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.GET("/list", r.groupHandler.ListGroups)
			routes.POST("/info", r.jidValidationMiddleware.ValidateNumberField(), r.groupHandler.GetGroupInfo)
			routes.POST("/invitelink", r.jidValidationMiddleware.ValidateNumberField(), r.groupHandler.GetGroupInviteLink)
			routes.POST("/photo", r.jidValidationMiddleware.ValidateNumberField(), r.groupHandler.SetGroupPhoto)
			routes.POST("/name", r.jidValidationMiddleware.ValidateNumberField(), r.groupHandler.SetGroupName)
			routes.POST("/description", r.jidValidationMiddleware.ValidateNumberField(), r.groupHandler.SetGroupDescription)
			routes.POST("/create", r.jidValidationMiddleware.ValidateMultipleNumbers("participants"), r.groupHandler.CreateGroup)
			routes.POST("/participant", r.jidValidationMiddleware.ValidateMultipleNumbers("participants"), r.groupHandler.UpdateParticipant)
			routes.GET("/myall", r.groupHandler.GetMyGroups)
			routes.POST("/join", r.groupHandler.JoinGroupLink)
			routes.POST("/inviteinfo", r.groupHandler.GetInviteInfo)
			routes.POST("/joininvite", r.groupHandler.JoinGroupInvite)
			routes.POST("/leave", r.jidValidationMiddleware.ValidateNumberField(), r.groupHandler.LeaveGroup)
			routes.POST("/settings", r.jidValidationMiddleware.ValidateNumberField(), r.groupHandler.UpdateGroupSettings)
			routes.POST("/requests", r.groupHandler.GetGroupRequests)
			routes.POST("/requests/update", r.jidValidationMiddleware.ValidateMultipleNumbers("participants"), r.groupHandler.UpdateGroupRequests)
		}
	}
	routes = eng.Group("/call")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/reject", r.jidValidationMiddleware.ValidateNumberField(), r.callHandler.RejectCall)
			routes.GET("/active", r.callHandler.ActiveCalls)
			routes.GET("/history", r.callHandler.History)
			routes.DELETE("/history", r.callHandler.DeleteHistory)
			routes.GET("/:callId", r.callHandler.GetCall)
			routes.POST("/answer", r.callHandler.AnswerCall)
			routes.POST("/dial", r.jidValidationMiddleware.ValidateNumberField(), r.callHandler.DialCall)
			routes.POST("/hangup", r.callHandler.HangupCall)
			routes.POST("/stream-ticket", r.callHandler.StreamTicket)
			routes.POST("/video", r.callHandler.VideoCall)
		}
	}
	routes = eng.Group("/community")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/create", r.communityHandler.CreateCommunity)
			routes.POST("/add", r.jidValidationMiddleware.ValidateJIDFields("number", "communityId"), r.communityHandler.CommunityAdd)
			routes.POST("/remove", r.jidValidationMiddleware.ValidateJIDFields("number", "communityId"), r.communityHandler.CommunityRemove)
		}
	}
	routes = eng.Group("/label")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/chat", r.jidValidationMiddleware.ValidateNumberField(), r.labelHandler.ChatLabel)
			routes.POST("/message", r.labelHandler.MessageLabel)
			routes.POST("/edit", r.labelHandler.EditLabel)
			routes.GET("/list", r.labelHandler.GetLabels)
		}
	}
	routes = eng.Group("/unlabel")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/chat", r.jidValidationMiddleware.ValidateNumberField(), r.labelHandler.ChatUnlabel)
			routes.POST("/message", r.labelHandler.MessageUnlabel)
		}
	}
	routes = eng.Group("/newsletter")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.POST("/create", r.newsletterHandler.CreateNewsletter)
			routes.GET("/list", r.newsletterHandler.ListNewsletter)
			routes.POST("/info", r.jidValidationMiddleware.ValidateJIDFields("newsletterId"), r.newsletterHandler.GetNewsletter)
			routes.POST("/link", r.jidValidationMiddleware.ValidateJIDFields("newsletterId"), r.newsletterHandler.GetNewsletterInvite)
			routes.POST("/subscribe", r.jidValidationMiddleware.ValidateJIDFields("newsletterId"), r.newsletterHandler.SubscribeNewsletter)
			routes.POST("/messages", r.jidValidationMiddleware.ValidateJIDFields("newsletterId"), r.newsletterHandler.GetNewsletterMessages)
			routes.POST("/follow", r.newsletterHandler.FollowNewsletter)
			routes.POST("/unfollow", r.newsletterHandler.UnfollowNewsletter)
			routes.POST("/mute", r.newsletterHandler.MuteNewsletter)
			routes.POST("/markviewed", r.newsletterHandler.MarkNewsletterViewed)
			routes.POST("/react", r.newsletterHandler.ReactNewsletter)
		}
	}

	// NOVO: Rotas de Enquetes (Polls)
	routes = eng.Group("/polls")
	{
		routes.Use(r.authMiddleware.Auth)
		{
			routes.GET("/:pollMessageId/results", r.pollHandler.GetPollResults)
		}
	}

}

func NewRouter(
	authMiddleware auth_middleware.Middleware,
	instanceHandler instance_handler.InstanceHandler,
	userHandler user_handler.UserHandler,
	sendHandler send_handler.SendHandler,
	messageHandler message_handler.MessageHandler,
	chatHandler chat_handler.ChatHandler,
	groupHandler group_handler.GroupHandler,
	callHandler call_handler.CallHandler,
	communityHandler community_handler.CommunityHandler,
	labelHandler label_handler.LabelHandler,
	newsletterHandler newsletter_handler.NewsletterHandler,
	pollHandler *poll_handler.PollHandler,
	serverHandler server_handler.ServerHandler,
) *Routes {
	return &Routes{
		authMiddleware:          authMiddleware,
		jidValidationMiddleware: auth_middleware.NewJIDValidationMiddleware(),
		instanceHandler:         instanceHandler,
		userHandler:             userHandler,
		sendHandler:             sendHandler,
		messageHandler:          messageHandler,
		chatHandler:             chatHandler,
		groupHandler:            groupHandler,
		callHandler:             callHandler,
		communityHandler:        communityHandler,
		labelHandler:            labelHandler,
		newsletterHandler:       newsletterHandler,
		pollHandler:             pollHandler,
		serverHandler:           serverHandler,
	}
}
