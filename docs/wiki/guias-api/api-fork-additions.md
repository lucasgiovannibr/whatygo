# Endpoints e opções adicionados no fork

Complementos à API do upstream (v0.7.2). Todos usam o header `apikey` da instância, exceto `GET /instance/proxy/{instanceId}`, que exige a **chave global**.

## Mensagens

### Mídia "ver uma vez" — `POST /send/media`

Novo campo opcional `viewOnce` (booleano). Vale para imagem, vídeo, áudio e vídeo-nota; documentos não suportam.

```json
{ "number": "5511999999999", "type": "image", "url": "https://exemplo.com/foto.jpg", "viewOnce": true }
```

### Votar em enquete — `POST /send/pollVote`

A instância precisa ter enviado ou recebido a enquete (o segredo da mensagem fica no store do whatsmeow).

```json
{
  "number": "120363000000000000@g.us",
  "pollMessageId": "3EB0DBF1C91EA77B149327",
  "participant": "5511888888888@s.whatsapp.net",
  "selectedOptions": ["Opção 1"]
}
```

| Campo | Descrição |
|---|---|
| `number` | Chat da enquete (número ou JID de grupo) |
| `pollMessageId` | ID da enquete (o `messageId` devolvido por `/send/poll`) |
| `participant` | Autor da enquete quando **não** foi esta instância. Obrigatório em grupos; em conversa 1:1 o padrão é `number` |
| `fromMe` | `true` quando a enquete foi enviada por esta instância |
| `selectedOptions` | Nomes das opções, exatamente como na enquete. Lista vazia remove o voto |

### Texto da citação — `quoted.text`

Campo opcional em qualquer envio com `quoted`. É o texto exibido no card da resposta; sem ele o WhatsApp mostra o card vazio.

### Assinar presença de um contato — `POST /message/subscribe`

```json
{ "number": "5511999999999" }
```

Passa a receber eventos `Presence` (online/offline/último visto) do contato (assinatura `PRESENCE`). O WhatsApp só entrega presença de outros enquanto a instância está "disponível"; por isso a instância fica disponível ao assinar e, **se `alwaysOnline` estiver desligado**, volta a "indisponível" após 2 minutos para não silenciar as notificações do celular. As assinaturas são perdidas ao reconectar. O evento agora traz também `from` e, quando conhecido, `lastSeen` (Unix).

## Usuário

### LID → telefone — `POST /user/lid`

```json
{ "lid": "123456789012345@lid", "groupJid": "120363000000000000@g.us" }
```

Consulta o mapeamento local do whatsmeow (o WhatsApp não tem consulta de servidor LID→telefone). `groupJid` é opcional: se o mapeamento não existir, a lista de participantes desse grupo é atualizada e a consulta repetida.

### Salvar contato — `POST /user/contacts`

```json
{ "phone": "5511999999999", "fullName": "Maria Silva", "firstName": "Maria", "saveOnPrimaryAddressbook": true }
```

Cria/atualiza o contato na lista do WhatsApp (app state) e, por padrão, pede ao aparelho principal que o grave também na agenda (`saveOnPrimaryAddressbook`, opcional; use `false` para manter só na lista interna). **Não existe remoção via API**: mutações de app state só gravam. Depois da sincronização ele aparece em `GET /user/contacts`.

### `PictureURL` em `POST /user/info`

Cada usuário passa a trazer `PictureURL` (além de `PictureID`). É best effort: as consultas dividem um orçamento de 5 s por chamada e param ao atingir limite de taxa do WhatsApp, então um lote grande nunca trava a resposta. Erros do WhatsApp agora saem como 429 (limite de taxa) ou 504 (timeout).

## Grupos

- `POST /group/requests` — lista quem aguarda aprovação para entrar (`{"groupJid": "...@g.us"}`).
- `POST /group/requests/update` — aprova ou rejeita (`{"groupJid": "...@g.us", "action": "approve", "participants": ["5511999999999"]}`; `action`: `approve` | `reject`).
- Os JIDs de participantes de `create`, `participant` e `requests/update` passam a ser enviados em forma canônica (sem `+`).

## Instância

### Status do proxy — `GET /instance/proxy/{instanceId}` (chave global)

```json
{
  "configured": true,
  "source": "instance",
  "protocol": "http",
  "host": "proxy.exemplo.com",
  "port": "8080",
  "hasAuth": true,
  "failClosed": false,
  "runtimeEnabled": true,
  "fallbackWithoutProxy": false,
  "lastAppliedAt": "2026-09-29T14:00:00Z"
}
```

Diz se o proxy está configurado **e** em uso pelo cliente em execução. Nunca devolve usuário, senha ou URL com credenciais. `fallbackWithoutProxy: true` significa que a autenticação no proxy falhou e o cliente conectou direto (expondo o IP do servidor). Para impedir isso, defina `PROXY_FAIL_CLOSED=true`: com ela o cliente **não conecta** quando o proxy falha.

### QR junto do passkey — `GET /instance/qr`

Quando há QR disponível, `qrcode`/`code` continuam vindo junto dos campos `passkey*`.

## Extensão `passkey-helper` 1.1.0

A chamada WebAuthn passou para o mundo `MAIN` da página, o que faz gerenciadores de senha (1Password, Bitwarden) aparecerem em vez de "insira sua chave de segurança". Requer Chrome/Edge 111+. Detalhes em `passkey-helper/README.md`.

## Saúde e diagnóstico

### `GET /health` — prontidão (pública)

Complementa `GET /server/ok`, que só diz que o processo está de pé (liveness) e continua sempre `200`.

```json
{ "status": "ok", "checks": { "usersDb": "ok", "authDb": "ok" } }
```

Cada verificação é `ok`, `slow` (o ping respondeu, mas em mais de 500 ms: é o que um pool esgotado ou sobrecarregado parece) ou `error` (falhou ou passou de 2 s). Qualquer `error` devolve **503** e `status: "unavailable"`; `slow` devolve 200 com `status: "degraded"`. As verificações rodam em paralelo (teto ~2 s). A rota é pública como `/server/ok` e só expõe estados, sem contagens nem identificadores.

Use `/server/ok` como *liveness* e `/health` como *readiness* no orquestrador: reiniciar o container porque o banco está lento não ajuda.

### `GET /instance/{instanceId}/runtime` — diagnóstico de uma instância

Chave global **ou** token da própria instância. Compara o que o banco diz com o que **este processo está executando**:

```json
{
  "instanceId": "…",
  "database": { "connected": true, "jid": "5531…:82@s.whatsapp.net", "alwaysOnline": false },
  "runtime": {
    "clientRegistered": true, "websocketConnected": true, "loggedIn": true,
    "deviceJid": "5531…:82@s.whatsapp.net",
    "runtimeActive": true, "killChannel": true, "supervisorCurrent": true,
    "reconnectInProgress": false,
    "qrCount": 0, "qrMax": 5, "passkeyCeremonyActive": false,
    "connectedSince": "…", "lastEventType": "Receipt", "lastEventAt": "…", "eventsSeen": 1234,
    "proxy": { "runtimeEnabled": true, "fallbackWithoutProxy": false }
  },
  "warnings": []
}
```

`warnings` lista cada inconsistência com um `code` estável:

| code | significa |
|---|---|
| `runtime_without_client` | um runtime é dono da instância, mas não há cliente (normal por alguns segundos ao iniciar) |
| `client_without_runtime` | cliente registrado sem supervisor: o kill/teardown de QR não o alcança |
| `supervisor_mismatch` | o estado do supervisor não pertence ao cliente registrado |
| `no_kill_channel` | Disconnect/teardown de QR não conseguem parar o runtime |
| `paired_but_offline` | dispositivo pareado, websocket caído e nenhuma reconexão em andamento |
| `qr_limit_near` | aguardando leitura do QR e perto do limite (o runtime vai reiniciar) |
| `db_connected_runtime_offline` / `db_disconnected_runtime_online` | banco e runtime discordam |
| `paired_in_db_unpaired_runtime` | o banco tem um JID pareado, mas o cliente em execução é um dispositivo novo (sessão perdida ou substituída) |
| `paired_without_runtime` | instância pareada sem nada rodando (chame `/instance/connect`) |
| `runtime_for_deleted_instance` | o processo ainda executa algo para uma instância que não existe mais |

### `GET /instance/runtimes` — todas as instâncias (chave global)

Devolve o mesmo diagnóstico de cada instância (inclusive de runtimes sem linha no banco), um `summary` (`instances`, `connected`, `withWarnings`) e estatísticas do processo (`uptimeSeconds`, `goroutines`, `heapAllocMb`, `sysMb`, `numGc`, `goVersion`). Um número de goroutines que só cresce é como um vazamento aparece.

### `ENABLE_PPROF=true` — profiler (opcional)

Expõe `/debug/pprof/*` (goroutine, heap, cpu…) **somente com a chave global**. Desligado por padrão. Ex.: `GET /debug/pprof/goroutine?debug=1` lista as pilhas agrupadas.

## Eventos de conexão novos

Três eventos que o whatsmeow já emitia e o projeto ignorava (só aparecia "Unhandled event" no log). Chegam pela assinatura **`CONNECTION`** (webhook, RabbitMQ, NATS, WebSocket e filas globais), como os demais eventos de conexão, e também aparecem em `GET /instance/{id}/runtime`.

### `ReachoutTimelock` — conta restrita para iniciar conversas

O WhatsApp restringiu a conta: ela não pode iniciar conversa com quem nunca falou com ela. Enviar para esse contato falha com o erro **463** (`NackCallerReachoutTimelocked`). Contatos que já conversaram continuam funcionando.

```json
{ "event": "ReachoutTimelock", "data": { "active": true, "enforcementType": "…", "endsAt": "2026-10-01T12:00:00Z" } }
```

`active: false` avisa que a restrição foi levantada. `endsAt` só aparece quando o WhatsApp informa o fim. Além do evento:

- o **erro do envio** deixa de ser "server returned error 463" e passa a explicar o que houve (e até quando, se a restrição é conhecida). O texto original continua na mensagem;
- o diagnóstico mostra `runtime.reachoutTimelock` e o aviso `reachout_timelock_active`.

O estado fica em memória: depois de reiniciar o processo ele só volta quando o WhatsApp o enviar de novo.

### `StreamError` — erro de stream desconhecido

```json
{ "event": "StreamError", "data": { "code": "…", "raw": "{…}" } }
```

`<stream:error>` com um código que a lib não conhece (os conhecidos viram outros eventos). É o que antecede a conexão morta do issue #185 do upstream. O diagnóstico traz `runtime.lastStreamError` e, por 30 minutos, o aviso `recent_stream_error`.

### `ClientOutdated` — versão do cliente recusada (405)

```json
{ "event": "ClientOutdated", "data": { "versionPinned": false } }
```

O WhatsApp recusou a versão do cliente. O projeto **descarta o cache da versão** (que valia 1 hora e faria todas as novas tentativas repetirem a versão recusada) para a próxima reconexão buscar a atual. Se `versionPinned` for `true`, as variáveis `WHATSAPP_VERSION_*` fixam a versão e precisam ser atualizadas ou removidas. O diagnóstico traz `runtime.clientOutdatedAt` e, por 30 minutos, o aviso `client_outdated`.

## Eventos de pareamento e de estado de chat

Eventos que o whatsmeow emitia e o projeto ignorava. Todos usam o mesmo envelope dos demais (`event`, `data`, `instanceId`, `instanceName`; `instanceToken` só com `WEBHOOK_INCLUDE_TOKEN=true`).

### Falhas de pareamento (assinatura `QRCODE`)

| Evento | Quando | `data` |
|---|---|---|
| `PairError` | O servidor confirmou o pareamento, mas concluí-lo localmente falhou | `id`, `lid`, `businessName`, `platform`, `error` |
| `QRScannedWithoutMultidevice` | O QR foi lido por um celular **sem multi-dispositivo**; o mesmo QR continua válido depois de ativá-lo | `message` |

Antes o usuário ficava sem nenhum retorno quando o pareamento não completava. `CATRefreshError` (`data.error`) segue a assinatura `CONNECTION`. `ManualLoginReconnect` só existe com `DisableLoginAutoReconnect`, que o projeto nunca liga, e por isso não é tratado.

### Mudanças de estado de chat feitas em outro aparelho (assinatura `CHAT_PRESENCE`)

Onde o `Archive` já era publicado. Uma integração (CRM, caixa de entrada) passa a saber que um chat foi fixado, silenciado, marcado como lido, limpo ou apagado.

| Evento | `data` |
|---|---|
| `Mute` | `jid`, `timestamp`, `muted`, `muteEndTimestamp` |
| `Pin` | `jid`, `timestamp`, `pinned` |
| `Star` | `chatJid`, `senderJid`, `isFromMe`, `messageId`, `timestamp`, `starred` |
| `MarkChatAsRead` | `jid`, `timestamp`, `read` |
| `ClearChat` / `DeleteChat` | `jid`, `timestamp`, `deleteMedia` |
| `DeleteForMe` | `chatJid`, `senderJid`, `isFromMe`, `messageId`, `timestamp`, `deleteMedia` |
| `UnarchiveChatsSetting` | `timestamp`, `unarchiveChats` |
| `UserStatusMute` | `jid`, `timestamp`, `muted` |

**Eventos de full sync não são publicados.** Logo depois de um pareamento o celular reenvia todo o seu estado (cada chat fixado, silenciado ou com estrela). Publicar isso inundaria o assinante com milhares de "mudanças" que não são mudanças; por isso só o que acontece depois vai para os webhooks.

## Mensagens temporárias (issue #79)

Quando um chat tem mensagens temporárias, toda mensagem enviada a ele precisa levar o timer (`ContextInfo.Expiration`); sem isso o destinatário vê "esta mensagem não vai desaparecer". A lib não faz isso no envio e **não permite ler** o timer de um chat, então o projeto o **aprende**:

- de `ContextInfo.Expiration` das mensagens recebidas (ou enviadas de outro aparelho);
- da mensagem de protocolo `EPHEMERAL_SETTING`, quando o timer muda (a única fonte para "desligado");
- de `GroupInfo`/`JoinedGroup` (`Ephemeral`);
- do próprio endpoint abaixo;
- **grupos**: se o timer é desconhecido (por exemplo, depois de reiniciar), o envio pergunta uma vez ao WhatsApp e guarda a resposta.

O que é aprendido fica em memória, por instância, e expira em 7 dias sem ser renovado (um timer desligado sem o projeto perceber deixa de ser aplicado). Num chat **individual** o timer só é conhecido depois de chegar uma mensagem dele ou de ser definido por aqui; até lá as mensagens saem sem timer, como antes. Um `Expiration` que o chamador já tenha colocado na mensagem prevalece. Para desligar a aplicação automática: `DISAPPEARING_AUTO_APPLY=false`.

### `POST /chat/disappearing`

```json
{ "chat": "5531999999999", "timer": "24h" }
```

`chat` é um contato ou um grupo (`...@g.us`). `timer`: `off`, `24h`, `7d` ou `90d` (o WhatsApp não aceita outros; valor inválido devolve 400). Resposta `{"message":"success"}`. Em grupo o servidor devolve o evento `GroupInfo`.

### `POST /user/defaultDisappearing`

```json
{ "timer": "off" }
```

Timer com o qual novos chats individuais começam.

## Grupo por convite

| Rota | Corpo | Uso |
|---|---|---|
| `POST /group/inviteinfo` | `{"code": "https://chat.whatsapp.com/XXXX"}` (aceita o código ou o link, com ou sem esquema/query) | Consulta o grupo **sem entrar**. Devolve a mesma estrutura de `/group/info` |
| `POST /group/inviteinfo` | `{"code","groupJid","inviter","expiration"}` | Mesmo, para o **cartão de convite** recebido num chat (`GroupInviteMessage`) |
| `POST /group/joininvite` | `{"code","groupJid","inviter","expiration"}` | Entra a partir do cartão de convite. Para link use `/group/join` |

O convite por cartão exige `groupJid` e `inviter`; sem `groupJid`, `/group/joininvite` responde 400 em vez de tentar entrar.

## Canais (newsletters)

Todas recebem `jid` do canal (`...@newsletter`); outro tipo de JID devolve 400.

| Rota | Corpo |
|---|---|
| `POST /newsletter/follow` | `{"jid"}` |
| `POST /newsletter/unfollow` | `{"jid"}` |
| `POST /newsletter/mute` | `{"jid","mute":true}` |
| `POST /newsletter/markviewed` | `{"jid","serverIds":[12,13]}` — conta uma visualização por mensagem; não marca o canal como lido nos outros aparelhos |
| `POST /newsletter/react` | `{"jid","serverId":12,"reaction":"👍"}` — `reaction` vazio remove a reação; `messageId` é opcional |

As chamadas têm um limite de 20 s (a lib espera a resposta do `markviewed` sem prazo).

## Mensagem que não chegou (`UndecryptableMessage` e `POST /message/rerequest`)

Quando uma mensagem chega e este aparelho não consegue decifrá-la, o projeto só escrevia uma linha de log. Agora o evento **`UndecryptableMessage`** é publicado sob a assinatura `MESSAGE` (as mensagens de "ver uma vez" continuam saindo como `Message`):

| Campo de `data` | Significado |
|---|---|
| `id`, `chat`, `sender`, `isGroup`, `isFromMe`, `timestamp`, `pushName` | Identificam a mensagem que faltou |
| `isUnavailable` | `true` se o remetente nem chegou a enviar o conteúdo a este aparelho |
| `unavailableType`, `decryptFailMode` | Tipos que são intencionalmente indisponíveis / ocultos |

### `POST /message/rerequest`

Pede ao celular uma nova cópia da mensagem.

```json
{ "chat": "120363000000000001@g.us", "sender": "5511999999999@s.whatsapp.net", "messageId": "3EB0..." }
```

`sender` é opcional num chat individual (é o próprio chat) e **obrigatório em grupo**; canal e status devolvem 400. Resposta: `{"data":{"requestId":"..."}}`. A resposta do celular chega depois como um evento `Message` normal cujo `UnavailableRequestID` é esse `requestId`. Não há garantia: o celular precisa estar online e ainda ter a mensagem.

`REREQUEST_FROM_PHONE=true` faz a lib pedir sozinha; a rota serve para quem prefere decidir (por exemplo, depois de ver o evento).

## Rotas de chat (`/chat/*`)

`/chat/pin`, `/chat/unpin`, `/chat/archive`, `/chat/unarchive`, `/chat/mute` e `/chat/unmute` usam o campo **`chat`** (não `number`): um número ou um JID de grupo. Em contatos individuais o número é resolvido para o LID com o qual o celular conhece o chat; antes o patch ia para um chat inexistente e nada acontecia.

```json
{ "chat": "5531999999999" }
```

`/chat/mute` aceita `duration` opcional: `8h`, `1w` (ou `7d`), `always` ou uma duração como `30m`. Sem ela, silencia por 1 hora, como sempre. A resposta traz o `timestamp` real (`{"data":{"timestamp":"2026-09-29T22:14:10Z"}}`); entrada inválida devolve 400.

## `POST /message/subscribe` em lote

`number` aceita um texto (como sempre) ou uma lista de até 100 números. Com lista, a resposta traz o resultado de cada um:

```json
{ "message": "success", "data": [{"number":"...","subscribed":true}], "failed": [] }
```

Só devolve erro (500) quando **nenhum** número foi inscrito.

## Presença ao mudar `alwaysOnline`

`PUT /instance/{id}/advanced-settings` com `alwaysOnline` diferente do atual marca a presença (`available`/`unavailable`) e inicia ou para o agendador imediatamente; antes só valia na próxima reconexão.

## `webhookUrl` em `POST /instance/connect`

| Valor | Efeito |
|---|---|
| ausente ou `""` | mantém o webhook atual (o manager envia `""` em toda reconexão) |
| uma URL | define o webhook |
| `"disabled"` ou `"false"` | **limpa** o webhook (guardado vazio) |

## Fila de entrega de webhooks

Cada URL de destino tem uma fila limitada; os eventos saem por poucos trabalhadores e, se o receptor falha, são reenviados com espera crescente (1 s, 5 s, 30 s, 2 min). Com a fila cheia, o evento **mais antigo** é descartado e contado. Depois que um evento esgota as tentativas, o destino fica "degradado" e os eventos seguintes têm uma tentativa cada, até um passar.

| Variável | Padrão | Efeito |
|---|---|---|
| `WEBHOOK_QUEUE_MAX_EVENTS` | 1000 | eventos pendentes por destino |
| `WEBHOOK_QUEUE_MAX_MB` | 64 | bytes pendentes por destino |
| `WEBHOOK_QUEUE_WORKERS` | 4 | entregas simultâneas por destino; `1` mantém a ordem estrita |

`GET /instance/runtimes` mostra o estado no bloco `webhook`: `destinations`, `degradedDestinations`, `pending`, `inFlight`, `pendingBytes`, `sent`, `failed` e `dropped` (contam desde que o processo subiu).

## Apagar uma instância

`DELETE /instance/delete/{id}` agora também remove do banco de autenticação o dispositivo pareado (chaves, contatos em cache, mapa de LID) e os votos de enquete da instância. Se a instância não estava conectada, o WhatsApp não é avisado: a sessão continua listada em "aparelhos conectados" no celular e precisa ser removida por lá.

## Gravar webhook e eventos sem conectar — `PUT /instance/{id}/integrations`

Grava o webhook, as assinaturas (`subscribe`) e as chaves de RabbitMQ, WebSocket e NATS **sem iniciar a instância** (o `POST /instance/connect` fazia isso, mas conectava junto). Se a instância está rodando, vale na hora. Campo vazio mantém o valor; `webhookUrl: "disabled"` remove o webhook; `subscribe: ["ALL"]` assina tudo. Detalhes em [API de Instâncias](./api-instances.md#gravar-webhook-eventos-e-produtores).

## Chamadas (atender, discar e vídeo) — experimental

Com `callsEnabled` ligado na instância (`PUT /instance/{id}/advanced-settings`, vale na próxima conexão), o servidor passa a **atender, discar, desligar e controlar o vídeo** de chamadas do WhatsApp, e leva o áudio (PCM 16 kHz) e o vídeo (H.264) por um **WebSocket** por chamada. Antes só existia `POST /call/reject`.

- Rotas novas: `GET /call/active`, `GET /call/{callId}`, `POST /call/answer`, `POST /call/dial`, `POST /call/hangup`, `POST /call/stream-ticket`, `GET /call/stream/{callId}` (WebSocket) e `POST /call/video`.
- Eventos novos (assinatura `CALL`): `CallReady`, `CallEnded` (com `reason`: `peer_hangup`, `hangup`, `rejected`, `rejected_busy`, `ring_timeout`, `stream_closed`, `server:<código>`) e `CallVideoState` (com `state` e `stateCode`).
- Variáveis: `CALL_MAX_CONCURRENT` (4), `CALL_RING_TIMEOUT` (90 s), `CALL_STREAM_GRACE` (10 s), `CALL_DIAL_LIMIT` (6/min) e `CALL_STREAM_ORIGINS`.
- Não funciona em instância com **proxy** (a mídia sairia por UDP direto, ignorando o proxy); `GET /call/active` mostra `state: "blocked_by_proxy"` e `GET /instance/runtimes` traz o aviso `calls_blocked_by_proxy`.
- Com o motor ligado, toda chamada recebida é **pré-aceita** automaticamente pela biblioteca.
- Validado ao vivo com um número real e um iPhone: áudio e vídeo, recebidos e enviados, e o upgrade de áudio para vídeo nos dois sentidos. O vídeo recebido traz a rotação em `orientation` (giros horários para ficar em pé); o vídeo enviado deve ser **em pé** (360×640) para ocupar a tela do celular.
- `enable` em `POST /call/video` só reativa vídeo que já existia: numa chamada que nunca teve vídeo devolve `409` (use `start`).
- **Melhorias do stream** (02/10/2026; tudo em [API de Chamadas](./api-call.md)): formato de áudio escolhido no bilhete (`encoding`, `sampleRate`: PCM 8/16/24 kHz, μ-law e A-law 8 kHz); quadros binários (`binary`); `timestamp` em todo quadro de mídia; eventos `speech_start` e `speech_end` (`speechEvents`); `mark` para saber até onde o áudio enviado foi tocado; fila de entrada de 900 ms (era 3 s) com aviso `inbound_overflow`; o áudio enfileirado enquanto o telefone toca espera e toca quando o outro lado atende.
- **Vigilância e limites**: evento `CallMediaStalled` / `CallMediaResumed` (`CALL_MEDIA_STALL`, `CALL_MEDIA_STALL_HANGUP`), duração máxima (`CALL_MAX_DURATION`) e timeout por silêncio (`CALL_SILENCE_TIMEOUT`), ambos desligados por padrão.
- **Histórico de chamadas** (`CALL_HISTORY=true`, `CALL_HISTORY_RETENTION_DAYS`, padrão 90): `GET`/`DELETE /call/history`, só metadados, nunca áudio. **As chamadas não são gravadas.**
- **Métricas** `whatygo_call*` em `GET /metrics`.

Guia completo, protocolo do WebSocket e exemplos em [API de Chamadas](./api-call.md).

## Painel (manager)

O painel em `/manager` foi refeito do zero (React 19, TypeScript, Vite e Tailwind, com tema claro e escuro). O código-fonte está em `manager/` e o resultado do build, versionado, em `manager/dist`. Como gerar e a estrutura estão em `manager/README.md`.

## Demais eventos do whatsmeow

Passam a ser publicados (antes só apareciam como "Unhandled event" no log):

| Assinatura | Evento | `data` |
|---|---|---|
| `CONTACT` | `Blocklist` | `action`, `dhash`, `prevDhash`, `changes` (`jid`, `action`). `action: "modify"` sem `changes` significa buscar a lista de novo em `GET /user/blocklist` |
| `CONTACT` | `PrivacySettings` | `settings` (valores novos) e `changed` (`groupAdd`, `lastSeen`, `status`, `profile`, `readReceipts`, `online`, `callAdd`, `messages`, `defense`, `stickers`) |
| `CONTACT` | `BusinessName` | `jid`, `oldBusinessName`, `newBusinessName` (e `messageId`/`chat` se a mudança foi vista numa mensagem) |
| `CALL` | `CallPreAccept`, `CallTransport` | `from`, `timestamp`, `callCreator`, `callCreatorAlt`, `callId`, `groupJid`, `remotePlatform`, `remoteVersion` |
| `CALL` | `CallReject` | os mesmos campos, sem `remote*` |
| `CALL` | `UnknownCallEvent` | `tag` e `attrs` do nó que a lib não reconheceu |
| `MESSAGE` | `MediaRetry` | `messageId`, `chat`, `sender`, `fromMe`, `timestamp`, `hasCiphertext`, `errorCode` (quando houver). O remetente precisa subir a mídia de novo; o texto cifrado não é publicado |
| `NEWSLETTER` | `NewsletterLiveUpdate` | `jid`, `time`, `messages` |
| `NEWSLETTER` | `NewsletterMuteChange` | `id`, `mute` |
| `CONNECTION` | `OfflineSyncPreview` | `total`, `messages`, `notifications`, `receipts`, `appDataChanges` (o que está na fila offline ao conectar) |

## Consultas: dispositivos, privacidade do status e perfil comercial

Todas são somente leitura e têm limite de 10 s.

| Rota | Corpo | Resposta |
|---|---|---|
| `POST /user/devices` | `{"number": "5531999999999"}` ou uma lista (até 50) | `data.devices`: `jid`, `user`, `device`, `server` (`device: 0` é o celular; o dispositivo desta instância não entra) e `data.count` |
| `GET /user/statusprivacy` | — | `data`: as configurações guardadas de "quem vê meu status" (`type`: `contacts`, `blacklist` ou `whitelist`, `list`, `isDefault`); a primeira é a padrão |
| `POST /user/business` | `{"number": "5531999999999"}` | `data`: perfil comercial (endereço, e-mail, categorias, horários). **404** quando o número não é uma conta Business |

## Espera pela conexão

Ao iniciar uma instância que não estava rodando, as rotas agora esperam a conexão em vez de dormir um tempo fixo: respondem assim que conecta, com limite de 10 s. Uma instância sem dispositivo pareado continua falhando na hora ("instance is not logged in"), e `GET /instance/qr` devolve o QR assim que ele existe.

## Rodada de endurecimento (outubro de 2026)

Mudanças de comportamento e novidades de operação; o texto completo está no [CHANGELOG](../../../CHANGELOG.md).

- **Erros com `code`**: toda falha responde `{"error", "code"}` com o status do tipo de falha (`503` sem conexão, `409` instância não pareada / desconectada pela API / em outra réplica, `429` com `Retry-After`...). Veja [Códigos de Erro](../referencia/error-codes.md).
- **Limite de envio por instância**: `SEND_MAX_CONCURRENT` (4) e `SEND_RATE_PER_MIN`; passar do limite por mais de `SEND_QUEUE_WAIT_SEC` (30) devolve `429 rate_limited` com `Retry-After`.
- **`POST /user/check`** aceita até 100 números por chamada, e "este número está no WhatsApp" é lembrado por `CHECK_USER_CACHE_TTL_MIN` (12 h; 5 min quando não está). Os envios seguintes ao mesmo número não consultam o WhatsApp de novo.
- **`POST /instance/disconnect`** desconecta de verdade e a instância só volta com `connect`/`reconnect`.
- **Eventos sem `instanceToken`** por padrão (`WEBHOOK_INCLUDE_TOKEN=true` restaura).
- **Senha do proxy** nunca volta nas respostas.
- **Mídia recebida** é baixada por trabalhadores limitados (`MEDIA_WORKERS`): uma mensagem com vídeo grande não atrasa as seguintes da instância. A ordem de chegada ao webhook pode então diferir entre uma mídia e um texto posterior (`MEDIA_ORDERED=true` volta ao modo sequencial); os eventos trazem o timestamp.
- **`GET /metrics`** (Prometheus, chave global) e `X-Request-ID` em toda resposta.
- **`GET /instance/{id}/logs`** devolve as linhas mais novas primeiro.
- **URLs recebidas só para endereços públicos** (`ALLOW_PRIVATE_URLS` libera as privadas), limite de corpo (`MAX_BODY_MB`/`MAX_MEDIA_BODY_MB`) e `CORS_ORIGINS`.
- **Várias réplicas**: uma réplica por instância (`INSTANCE_LOCK`); parada ordenada com `stop_grace_period: 30s`.
