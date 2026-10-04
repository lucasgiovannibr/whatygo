# API de Chamadas

Documentação dos endpoints de chamadas WhatsApp: rejeitar chamadas recebidas (sempre disponível) e, com o **motor de chamadas** ligado na instância, atender, discar, controlar o vídeo e trocar áudio e vídeo por um WebSocket.

> ⚠️ **Experimental.** O motor de chamadas usa a biblioteca [`purpshell/meowcaller`](https://github.com/purpshell/meowcaller) (fixada em um commit), que implementa a sinalização e a mídia de VoIP do WhatsApp por conta própria. Foi validado ao vivo com um número real e um iPhone (áudio e vídeo, recebidas e enviadas), mas o WhatsApp pode mudar o protocolo sem aviso. Não há chamadas em grupo, e **as chamadas não são gravadas**: o servidor só guarda, se você pedir, o histórico de quem ligou e como terminou.

## 📋 Índice

- [Dois modos](#dois-modos)
- [Ligar o motor de chamadas](#ligar-o-motor-de-chamadas)
- [Resumo dos endpoints](#resumo-dos-endpoints)
- [Receber uma chamada](#receber-uma-chamada)
- [Fazer uma chamada](#fazer-uma-chamada)
- [O stream (WebSocket)](#o-stream-websocket)
- [Opções do stream](#opções-do-stream)
- [Vídeo](#vídeo)
- [Eventos](#eventos)
- [Histórico de chamadas](#histórico-de-chamadas)
- [Limites e configuração](#limites-e-configuração)
- [Erros](#erros)
- [Rejeitar Chamada](#rejeitar-chamada)
- [No painel (aba Chamadas)](#no-painel-aba-chamadas)
- [Teste ao vivo](#teste-ao-vivo)

---

## Dois modos

| Modo | Como ligar | O que faz |
|------|-----------|-----------|
| **Só rejeitar** (padrão) | nada | Recebe o evento `CallOffer` e pode rejeitar com `POST /call/reject`. Não atende nem disca. |
| **Motor de chamadas** | `callsEnabled` na instância | Atende, disca, desliga, controla o vídeo e leva o áudio e o vídeo pelo stream. |

O motor é **opt-in por instância**. Com ele ligado, **toda chamada recebida é pré-aceita** automaticamente pela biblioteca (o aparelho de quem liga passa a mostrar a chamada como em preparação). Por isso ele não vem ligado.

---

## Ligar o motor de chamadas

```bash
curl -X PUT "http://localhost:4000/instance/ID-DA-INSTANCIA/advanced-settings" \
  -H "Content-Type: application/json" \
  -H "apikey: SUA-GLOBAL-API-KEY" \
  -d '{"callsEnabled": true}'
```

A mudança **só vale na próxima conexão** da instância: reinicie o servidor ou desconecte e conecte de novo. Depois confira com:

```bash
curl http://localhost:4000/call/active -H "apikey: TOKEN-DA-INSTANCIA"
```

```json
{ "enabled": true, "state": "active", "calls": [] }
```

| `state` | Significado |
|---------|-------------|
| `active` | Motor funcionando |
| `hook_failed` | O motor não conseguiu se acoplar ao whatsmeow; as chamadas ficam sem mídia (`error` traz o motivo) |
| `blocked_by_proxy` | A instância usa proxy. A mídia da chamada sai por UDP direto e **ignoraria o proxy** (vazaria o IP), então o motor fica desligado nessas instâncias |

`enabled: false` sem `state` significa que o cliente em execução não tem motor: as chamadas estão desligadas ou a instância ainda não reconectou depois de ligá-las.

`GET /instance/{instanceId}/runtime` e `GET /instance/runtimes` mostram `runtime.calls` (`state`, `activeCalls`) e avisos: `calls_enabled_pending_reconnect`, `calls_hook_failed` e `calls_blocked_by_proxy`.

---

## Resumo dos endpoints

Todos usam o **token da instância** no header `apikey`.

| Endpoint | Para quê |
|----------|----------|
| `GET /call/active` | Estado do motor e as chamadas que a instância acompanha |
| `GET /call/{callId}` | Uma chamada: fase, direção, vídeo, contadores do stream e `mediaStalled` |
| `GET /call/history` | Histórico de chamadas da instância (precisa de `CALL_HISTORY=true`) |
| `DELETE /call/history` | Apaga o histórico da instância |
| `POST /call/answer` | Atende uma chamada que está tocando |
| `POST /call/dial` | Liga para alguém |
| `POST /call/hangup` | Encerra uma chamada em qualquer fase |
| `POST /call/stream-ticket` | Gera o bilhete para abrir o stream |
| `GET /call/stream/{callId}?ticket=…` | **WebSocket** com o áudio e o vídeo |
| `POST /call/video` | Controles de vídeo (`start`, `accept`, `stop`, `enable`, `disable`, `orientation`) |
| `POST /call/reject` | Rejeita uma chamada recebida (vale sem o motor) |

**Fases** (`phase`): `calling` (discando), `ringing` (tocando), `connecting`, `active`, `ended`, `other`. **Direção**: `incoming` ou `outgoing`.

Exemplo de `GET /call/{callId}`:

```json
{
  "callId": "0018F2C9B00F33ACD31A60AEA2486853",
  "direction": "incoming",
  "peer": "273117121392855@lid",
  "phase": "active",
  "startedAt": "2026-09-30T16:22:02Z",
  "video": true,
  "videoSending": true,
  "videoReceiving": true,
  "peerVideo": { "active": true, "upgrade": false, "orientation": 1, "state": "enabled", "stateCode": 1 },
  "mediaStalled": false,
  "stream": { "attached": true, "toClient": 620, "fromClient": 610, "videoToClient": 150 }
}
```

---

## Receber uma chamada

1. O webhook recebe `CallOffer` (assinatura `CALL`) com o `callId`. Ou consulte `GET /call/active` e procure `direction: incoming` e `phase: ringing`.
2. `POST /call/stream-ticket` com `{"callId": "...", "video": true}` (use `video: true` só se for usar o vídeo). O bilhete aceita mais opções (formato de áudio, quadros binários, eventos de fala): ver [Opções do stream](#opções-do-stream).
3. Abra o WebSocket do bilhete **antes de atender**: o stream fica ligado enquanto a chamada ainda toca e nenhum áudio se perde. O áudio que você mandar antes de a chamada estar ativa **fica na fila e toca quando ela ficar**.
4. `POST /call/answer` com `{"callId": "..."}`.

```bash
curl -X POST http://localhost:4000/call/answer \
  -H "Content-Type: application/json" -H "apikey: TOKEN-DA-INSTANCIA" \
  -d '{"callId": "0018F2C9B00F33ACD31A60AEA2486853"}'
```

Uma chamada **atendida sem stream é desligada** depois de `CALL_STREAM_GRACE` segundos (motivo `stream_closed`). O mesmo vale se o stream de uma chamada em andamento fechar e não voltar nesse prazo.

---

## Fazer uma chamada

```bash
curl -X POST http://localhost:4000/call/dial \
  -H "Content-Type: application/json" -H "apikey: TOKEN-DA-INSTANCIA" \
  -d '{"number": "5511999990000", "video": true, "stream": true}'
```

```json
{
  "callId": "E2BDDB29A16F34D5CDCCA8101FD6F42E",
  "direction": "outgoing",
  "peer": "5511999990000@s.whatsapp.net",
  "phase": "calling",
  "video": true,
  "streamTicket": {
    "ticket": "…",
    "expiresInSeconds": 30,
    "path": "/call/stream/E2BDDB29A16F34D5CDCCA8101FD6F42E?ticket=…"
  }
}
```

- `video: true` faz uma **videochamada**.
- `stream: true` já devolve o bilhete (com vídeo se `video` for `true`), para ligar o WebSocket **antes** de o outro lado atender. Ele aceita as mesmas opções do bilhete de `POST /call/stream-ticket` (`encoding`, `sampleRate`, `binary`, `speechEvents`); um formato inválido é recusado com `400` **antes** de o telefone tocar.
- O áudio que você enfileirar enquanto o telefone toca (uma saudação, por exemplo) **espera** e toca quando o outro lado atende; um `mark` colocado depois dele só volta depois disso.
- Há um limite de chamadas por minuto e por instância (`CALL_DIAL_LIMIT`, padrão 6, contando as que falham): discar em massa é o que leva uma conta a ser sinalizada. Passou do limite, responde `429`.
- Instâncias com proxy não discam (ver `blocked_by_proxy`).

---

## O stream (WebSocket)

Abra `ws://host:4000` + o `path` do bilhete (`/call/stream/{callId}?ticket=…`).

- O **bilhete vale para um uso e expira em 30 s**. Ele evita pôr a chave de API na URL.
- Navegadores: a origem da página precisa estar em `CALL_STREAM_ORIGINS` (ou ser a do próprio servidor). Quem não manda `Origin` (um servidor, um script) é sempre aceito: o bilhete é que protege.
- O formato das mensagens segue o de Twilio Media Streams, JSON por mensagem.

**Servidor → cliente**

```json
{"event":"start","callId":"…","sampleRate":16000,"channels":1,"encoding":"audio/pcm-s16le","frameMs":60,
 "direction":"incoming","video":false,"videoStream":true,"binary":false,"speechEvents":false}
{"event":"media","track":"inbound","seq":1,"timestamp":1172,"payload":"<base64 PCM>"}
{"event":"video","track":"inbound","seq":1,"timestamp":1172,"keyframe":true,"orientation":1,"payload":"<base64 H.264>"}
{"event":"video_state","active":true,"upgrade":false,"orientation":1,"state":"enabled","stateCode":1}
{"event":"keyframe_request"}
{"event":"speech_start","timestamp":5311}
{"event":"speech_end","timestamp":8735,"durationMs":3423}
{"event":"mark","name":"saudacao"}
{"event":"error","code":"…","message":"…"}
{"event":"stop","reason":"peer_hangup"}
```

**Cliente → servidor**

```json
{"event":"media","payload":"<base64 PCM>"}
{"event":"video","payload":"<base64 H.264 access unit>"}
{"event":"mark","name":"saudacao"}
{"event":"clear"}
{"event":"stop"}
```

`speech_start` e `speech_end` só chegam em streams que pediram `speechEvents`; com `binary` o áudio e o vídeo trocam de formato (ver abaixo). Os demais eventos são sempre JSON.

**Áudio**: por padrão PCM 16 bits, little-endian, **16 kHz, mono**, em base64 (outros formatos em [Opções do stream](#opções-do-stream)). O servidor entrega quadros de 60 ms; você pode mandar pedaços de qualquer tamanho. `clear` descarta o que já estava na fila para o outro lado. A fila do que chega do outro lado guarda **900 ms**: se o cliente ler mais devagar que isso, o áudio mais antigo é descartado (contadores em `GET /call/{callId}`) e o stream avisa **uma vez** com `error` `inbound_overflow`, para o cliente não conversar com algo de três segundos atrás.

**Vídeo** (só em streams cujo bilhete pediu `video: true`; nos demais, mensagens `video` são ignoradas com um `error`):

- H.264 em **Annex-B** (códigos de início `00 00 01` / `00 00 00 01`), **um quadro (access unit) por mensagem**, com SPS e PPS na frente de cada keyframe. O formato MP4/AVCC (com prefixo de tamanho) é recusado.
- O servidor **não codifica nem decodifica**: só leva o H.264. Mande um keyframe primeiro e de novo **sempre que chegar `keyframe_request`** (o WhatsApp pede quando perde quadros).
- Para o vídeo que **chega**, um cliente que fica para trás perde a fila inteira e só volta a receber a partir do próximo keyframe (um quadro perdido estraga os seguintes).
- **Tamanho:** a tela da chamada no celular fica em pé. Mande vídeo **em pé** (por exemplo 360×640) para ocupar a tela toda; um vídeo deitado (640×360) aparece pequeno, com faixas.
- **Orientação do vídeo recebido:** o `orientation` de cada mensagem `video` é o número de **giros de 90° no sentido horário** que você deve aplicar à imagem para mostrá-la em pé (0 a 3). Ele acompanha a câmera em uso. O `orientation` de `video_state` é o do aparelho do outro lado e **não** serve para girar a imagem.

---

## Opções do stream

`POST /call/stream-ticket` (e `POST /call/dial` com `"stream": true`) aceitam estas opções; o bilhete as carrega para o WebSocket, a `start` confirma o que valeu e a resposta do bilhete devolve `encoding`, `sampleRate`, `binary` e `speechEvents`.

| Campo | Padrão | O que faz |
|-------|--------|-----------|
| `video` | `false` | O stream leva também o vídeo da chamada |
| `encoding` | `audio/pcm-s16le` | Codificação do áudio (tabela abaixo) |
| `sampleRate` | `16000` (`8000` em G.711) | Taxa do áudio |
| `binary` | `false` | Áudio e vídeo em quadros binários, não em base64 dentro de JSON |
| `speechEvents` | `false` | O stream avisa quando o outro lado começa e para de falar |

### Formato de áudio

A chamada em si roda sempre a 16 kHz; o servidor converte nos dois sentidos (um reamostrador que mantém o estado entre os pedaços, sem filtrar o que cairia como chiado ao baixar de 16 para 8 kHz). O quadro continua sendo de 60 ms (`frameMs: 60`), o que dá 480, 960 ou 1440 amostras conforme a taxa.

| `encoding` | Taxas | Também aceito como |
|------------|-------|--------------------|
| `audio/pcm-s16le` | `8000`, `16000`, `24000` | `pcm`, `pcm16`, `linear16` |
| `audio/x-mulaw` (G.711 μ-law) | `8000` | `mulaw`, `ulaw`, `pcmu`, `audio/pcmu` |
| `audio/x-alaw` (G.711 A-law) | `8000` | `alaw`, `pcma`, `audio/pcma` |

PCM a 24 kHz é o que a API Realtime da OpenAI usa por padrão; μ-law a 8 kHz é o da telefonia. Qualquer outra combinação (por exemplo `mulaw` a 16 kHz, ou `opus`) é recusada com `400`. O formato padrão não passa por nenhuma conversão.

### Timestamps

Todo `media` e `video` do servidor traz `timestamp`: **milissegundos desde a abertura do stream até o momento em que o quadro chegou ao servidor** (não quando o socket o escreveu). O áudio **não chega em ritmo constante**: um lado que fica em silêncio, ou com o microfone mudo, manda só dois ou três quadros por segundo. Quem grava precisa **posicionar cada quadro pelo `timestamp` e preencher os vazios com silêncio**, não emendar os quadros (uma chamada de 68 s dava um arquivo de 29 s).

### Quadros binários

Com `binary: true` o áudio e o vídeo, que são o que flui o tempo todo, saem do base64 e viram quadros binários do WebSocket: um terço menores e nada para codificar. Os inteiros são **big-endian**. O resto (`start`, `mark`, `video_state`, `keyframe_request`, `speech_*`, `error`, `stop`) continua JSON em texto.

| Sentido | Quadro |
|---------|--------|
| servidor → cliente, áudio | `0x01` · `seq` (u32) · `timestamp` (u32) · áudio no formato do stream |
| servidor → cliente, vídeo | `0x02` · `flags` (u8) · `seq` (u32) · `timestamp` (u32) · access unit H.264 |
| cliente → servidor, áudio | `0x01` · áudio |
| cliente → servidor, vídeo | `0x02` · access unit H.264 |

`flags` do vídeo: bit 0 = keyframe, bits 1 e 2 = `orientation` (0 a 3, o mesmo significado da mensagem JSON). Num stream binário o cliente **ainda pode** mandar `media` e `video` em JSON. Um quadro binário num stream que não pediu `binary` é recusado com `error` `binary_not_enabled`; um tipo desconhecido ou um quadro vazio, com `bad_binary`.

### Eventos de fala

Com `speechEvents: true` o stream avisa quando o outro lado começa e para de falar, para um cliente que responde por voz saber quando calar (`clear`) e quando responder, sem um detector próprio:

```json
{"event":"speech_start","timestamp":5311}
{"event":"speech_end","timestamp":8735,"durationMs":3423}
```

É um detector de energia que acompanha o ruído do lugar do outro lado: a fala começa depois de uns **120 ms** de voz seguida (um estalo não conta) e termina **600 ms** depois da última voz, o bastante para uma pausa entre palavras. O `timestamp` do `speech_end` é o último momento de fala, no mesmo relógio dos quadros, e o aviso chega ao cliente uns 0,6 s depois. O fim também é procurado pelo relógio, porque quem parou de falar manda quase nada. Uma chamada que acaba enquanto o outro lado fala recebe o `speech_end` **antes** do `stop`.

**Limite:** não é um reconhecedor de fala. Um ruído constante mais alto do que o lugar estava quando a chamada começou conta como fala até parar. Os valores (120 ms, 600 ms, o piso de −42 dBFS) não são configuráveis.

### `mark` e `clear`

O cliente manda `{"event":"mark","name":"saudacao"}` e recebe a **mesma mensagem de volta quando o áudio que mandou antes dela já foi entregue à chamada** (a biblioteca o envia ao outro lado em até um quadro, 60 ms). É assim que quem fala por voz sabe até onde o que enviou foi tocado quando é interrompido. Com a fila vazia, volta na hora; os `mark` voltam na ordem.

- `clear` devolve **todos** os `mark` que esperavam, na hora: o áudio antes deles foi descartado e não há mais o que esperar.
- No máximo **64** `mark` esperam ao mesmo tempo (`error` `too_many_marks` além disso). Os que ainda esperam quando a chamada acaba não são enviados.
- Antes de a chamada estar **ativa** (tocando, conectando) o áudio do cliente **não é consumido**: fica na fila e toca quando ela ativa; o `mark` só volta depois. A fila guarda até 30 s; além disso o áudio é descartado (`outbound_overflow`).

### Códigos de `error` do stream

| `code` | Quando |
|--------|--------|
| `bad_message` | O texto recebido não é um objeto JSON |
| `bad_payload` | O `payload` não é base64 válido |
| `bad_video`, `video_not_ready` | Vídeo que não é H.264 Annex-B, grande demais, ou a chamada ainda não manda vídeo |
| `video_not_enabled` | Vídeo num stream aberto sem `video` |
| `outbound_overflow` | O cliente mandou áudio mais rápido que o tempo real por tempo demais |
| `inbound_overflow` | O cliente lê mais devagar do que o áudio chega (avisa uma vez) |
| `too_many_marks` | Mais de 64 `mark` esperando |
| `binary_not_enabled`, `bad_binary` | Quadro binário num stream que não pediu, ou inválido |
| `stream_busy`, `call_not_found` | A chamada já tem um stream; ou não existe mais |

---

## Vídeo

`POST /call/video` com `{"callId": "...", "action": "..."}` numa chamada já atendida:

| `action` | O que faz |
|----------|-----------|
| `start` | Pede ao outro lado para transformar uma **chamada de áudio em vídeo**. O iPhone aceita e a chamada vira vídeo; depois disso você manda o vídeo pelo stream |
| `accept` | Aceita o pedido do outro lado (`video_state` com `upgrade: true`) |
| `stop` | Para de mandar vídeo; o áudio e o vídeo dele continuam |
| `disable` / `enable` | Silencia e reativa o **seu** vídeo |
| `orientation` | Informa a rotação da sua câmera (`orientation` de 0 a 3, giros horários) |

- Quando o **outro lado** liga a câmera numa chamada de áudio, o WhatsApp manda um pedido de upgrade (`video_state` com `upgrade: true`): responda com `accept`.
- **`enable` só reativa vídeo que já existia.** Numa chamada que nunca teve vídeo ele é **recusado com `409`** (o iPhone ignora "câmera ligada" sem o pedido de upgrade); use `start`.

**`video_state`** (mensagem do stream e evento `CallVideoState`): o campo `state` diz o que o outro lado sinalizou:

| `state` | `stateCode` | Significado |
|---------|-------------|-------------|
| `enabled` | 1 | Câmera ligada |
| `disabled` | 0 | Câmera silenciada |
| `stopped` | 6 | Parou de mandar vídeo |
| `upgrade_request` | 3 ou 11 | Pede para virar chamada de vídeo |
| `upgrade_accepted` | 4 | Aceitou o upgrade que você pediu |
| `upgrade_rejected` | 5 | Recusou o upgrade |
| `upgrade_cancelled` | 8 | Desistiu do pedido dele |
| `unknown` | outro | Estado sem nome; o número em `stateCode` identifica. O iPhone manda o **2** logo depois de a chamada ser atendida, antes do primeiro quadro (significado não confirmado) |

`active` e `upgrade` sozinhos não distinguem "aceitou o upgrade" de "desligou a câmera" (os dois ficam `false`); use `state`.

---

## Eventos

Com a assinatura `CALL`, além de `CallOffer`, `CallAccept`, `CallTerminate`, `CallOfferNotice`, `CallRelayLatency`, `CallPreAccept`, `CallReject`, `CallTransport` e `UnknownCallEvent` (da biblioteca do WhatsApp), o motor publica:

| Evento | Quando | Campos |
|--------|--------|--------|
| `CallReady` | A mídia da chamada subiu | `callId`, `peer`, `direction`, `video` |
| `CallEnded` | A chamada terminou | os anteriores, mais `reason` e `durationSeconds` |
| `CallVideoState` | O outro lado mudou o vídeo | `callId`, `peer`, `direction`, `video`, `active`, `upgrade`, `orientation`, `state`, `stateCode` |
| `CallMediaStalled` | Uma chamada ativa, com stream, ficou `CALL_MEDIA_STALL` s sem receber áudio do outro lado | `callId`, `peer`, `direction`, `video`, `idleSeconds`, `hangup` |
| `CallMediaResumed` | O áudio voltou depois de um `CallMediaStalled` | `callId`, `peer`, `direction`, `video` |

`CallMediaStalled` existe porque há casos conhecidos de a chamada continuar de pé no WhatsApp sem áudio (veja os issues da biblioteca): o consumidor do stream só ouviria silêncio. Só vale para chamada **ativa com stream** (sem stream o `CALL_STREAM_GRACE` já encerra). Um lado em silêncio ou com o microfone mudo **não** dispara o evento: ele continua mandando dois ou três quadros por segundo (verificado num iPhone). Por padrão o evento só avisa; `CALL_MEDIA_STALL_HANGUP=true` também desliga a chamada (`media_stalled`). `GET /call/{callId}` mostra `mediaStalled`.

**Motivos de `CallEnded` (`reason`)**

| `reason` | Quem encerrou |
|----------|---------------|
| `peer_hangup` | O outro lado desligou. O WhatsApp não manda motivo nesse caso; este nome cobre o "vazio" |
| `hangup` | Você desligou (`POST /call/hangup`) |
| `rejected` | A chamada foi rejeitada |
| `rejected_busy` | A instância já tinha `CALL_MAX_CONCURRENT` chamadas |
| `ring_timeout` | Ninguém atendeu em `CALL_RING_TIMEOUT` (só pega chamadas cujo fim nunca chegou) |
| `stream_closed` | A chamada em andamento ficou sem stream por mais de `CALL_STREAM_GRACE` |
| `media_stalled` | Ficou sem áudio do outro lado e `CALL_MEDIA_STALL_HANGUP` está ligado |
| `max_duration` | Passou de `CALL_MAX_DURATION` |
| `silence_timeout` | Ninguém fez barulho por `CALL_SILENCE_TIMEOUT` |
| `server:<código>` | Erro do servidor do WhatsApp |
| outro texto | Motivo enviado pelo próprio WhatsApp |

Uma chamada em que quem liga desiste **antes de ser atendida** também termina como `peer_hangup`; para saber que foi "perdida" use o `outcome` do [histórico](#histórico-de-chamadas).

---

## Histórico de chamadas

Com `CALL_HISTORY=true` o servidor guarda **uma linha por chamada** que o motor acompanhou, numa tabela `call_records` (criada só nesse caso). **Só metadados: nunca áudio, vídeo nem conteúdo.** Vale para as instâncias com `callsEnabled`. Vem **desligado**: o número do contato é dado pessoal.

**`GET /call/history`** (do mais novo para o mais velho; só as chamadas da instância do token):

| Parâmetro | O que faz |
|-----------|-----------|
| `direction` | `incoming` ou `outgoing` |
| `outcome` | Um dos resultados abaixo |
| `peer` | Um JID ou um telefone (com ou sem `+` e pontuação) |
| `limit` | 1 a 200 (padrão 50) |
| `cursor` | O `next` da página anterior |

```json
{
  "records": [
    {
      "id": "6f1c…",
      "callId": "0076EF64CD005F6021FF58EEABE5FFA2",
      "peer": "273117121392855@lid",
      "peerPhone": "553197157574",
      "direction": "incoming",
      "video": false,
      "outcome": "answered",
      "reason": "peer_hangup",
      "startedAt": "2026-10-02T16:45:38Z",
      "answeredAt": "2026-10-02T16:45:38Z",
      "endedAt": "2026-10-02T16:45:47Z",
      "talkSeconds": 9,
      "ringSeconds": 0
    }
  ],
  "next": "MTc1OTQyNzEz…"
}
```

- `peer` é o outro lado como o WhatsApp informa (em geral um `@lid`); `peerPhone` é o telefone (só dígitos) quando a conta consegue resolvê-lo, e some quando não.
- A paginação é por cursor (estável mesmo com chamadas novas entrando); `next` não vem na última página.
- Responde `409` se o servidor não guarda histórico e `400` para filtro ou cursor inválidos.

**Resultado (`outcome`)**

| `outcome` | Significa |
|-----------|-----------|
| `answered` | A mídia subiu: houve conversa (`talkSeconds`) |
| `missed` | Chamada recebida que ninguém atendeu (quem ligou desistiu, ou o `ring_timeout`) |
| `rejected` | Recusada, por este lado ou pelo outro numa chamada discada (o WhatsApp informa `rejected`) |
| `cancelled` | Chamada discada que este lado desligou antes de ser atendida |
| `unanswered` | Chamada discada que não foi atendida |
| `busy` | Chamada recebida recusada porque a instância já tinha `CALL_MAX_CONCURRENT` chamadas |
| `failed` | Terminou por erro ou por código do servidor |

`talkSeconds` conta da mídia pronta ao fim, e `ringSeconds` do começo até atender (ou até o fim). Verificado ao vivo: recebida perdida, recusada e atendida, e discada recusada, cancelada e atendida. **Não verificados num aparelho:** `unanswered` e `failed`.

**`DELETE /call/history`** apaga o histórico da instância (`{"deleted": N}`), todo ou só o que começou antes de `?before=` (RFC 3339, por exemplo `2026-09-01T00:00:00Z`). Não dá para desfazer. Uma data mal escrita é `400` e **não apaga nada**.

**Retenção:** os registros mais velhos que `CALL_HISTORY_RETENTION_DAYS` (padrão **90**; `0` guarda para sempre) são apagados ao iniciar e uma vez por dia. Uma falha ao gravar perde o registro, nunca a chamada (`evolution_call_history_failed_total`).

---

## Limites e configuração

| Variável | Padrão | O que faz |
|----------|--------|-----------|
| `CALL_MAX_CONCURRENT` | `4` | Chamadas ao mesmo tempo por instância; as recebidas além disso são rejeitadas (`rejected_busy`) e as discadas recusadas |
| `CALL_RING_TIMEOUT` | `90` (s) | Tempo até largar uma chamada que ninguém atendeu |
| `CALL_STREAM_GRACE` | `10` (s) | Quanto uma chamada atendida espera o stream voltar antes de ser desligada |
| `CALL_DIAL_LIMIT` | `6` | Chamadas discadas por minuto por instância |
| `CALL_STREAM_ORIGINS` | vazio | Origens de navegador aceitas no WebSocket, separadas por vírgula (`*` aceita todas) |
| `CALL_MEDIA_STALL` | `15` (s) | Quanto uma chamada ativa com stream pode ficar sem áudio do outro lado antes do evento `CallMediaStalled`; `0` desliga |
| `CALL_MEDIA_STALL_HANGUP` | `false` | Com `true`, uma chamada nessa situação também é desligada (`media_stalled`) |
| `CALL_MAX_DURATION` | `0` (sem limite) | Segundos que uma chamada atendida pode durar, contados da mídia pronta; passou, é desligada (`max_duration`) |
| `CALL_SILENCE_TIMEOUT` | `0` (nunca) | Segundos sem barulho de nenhum dos dois lados antes de desligar uma chamada com stream (`silence_timeout`) |
| `CALL_HISTORY` | `false` | Guarda o [histórico de chamadas](#histórico-de-chamadas) |
| `CALL_HISTORY_RETENTION_DAYS` | `90` | Dias que o histórico é guardado; `0` guarda para sempre |

Valores inválidos ou não positivos voltam ao padrão (exceto onde o `0` é a escolha, como em `CALL_MEDIA_STALL` e `CALL_HISTORY_RETENTION_DAYS`).

**Duração:** por padrão o servidor **não limita** quanto tempo dura uma chamada atendida. `CALL_MAX_DURATION` e `CALL_SILENCE_TIMEOUT` existem para a chamada que ninguém acompanha mais (um agente que travou sem desligar, um telefone esquecido) e que ocupa uma vaga da conta e uma sessão paga de modelo para sempre. "Barulho" é um quadro acima de uns −50 dBFS: o ruído de conforto de quem está mudo fica muito abaixo, e uma voz baixa (−40 dBFS) fica muito acima, então ninguém falando baixo é confundido com silêncio. O áudio do próprio cliente também conta. O timeout por silêncio só vale para chamada **ativa com stream**. Se o stream cair, a chamada espera `CALL_STREAM_GRACE` segundos para o cliente abrir outro (com um bilhete novo de `POST /call/stream-ticket`) e só então é desligada (`stream_closed`).

**Métricas** (`GET /metrics`, chave global): `evolution_calls_active{phase}`, `evolution_calls_started_total{direction,video}`, `evolution_calls_ended_total{direction,reason}`, `evolution_call_talk_seconds`, `evolution_call_dials_total{result}`, `evolution_call_engines{state}`, `evolution_call_streams_attached`, `evolution_call_media_stalled` e `evolution_call_media_stalls_total`, `evolution_call_stream_frames_total` e `evolution_call_stream_dropped_total` (`direction`, `kind`), `evolution_call_keyframe_requests_total`, `evolution_call_history_saved_total` e `evolution_call_history_failed_total`. Os rótulos são limitados de propósito: nunca a instância nem o número, e `server:<código>` e motivos em texto livre viram `server` e `other`.

---

## Erros

| Código | Quando |
|--------|--------|
| `400` | Corpo inválido; `callId` faltando; número para o qual não dá para ligar; ação ou orientação de vídeo desconhecidas; formato de áudio não suportado; filtro, cursor, `limit` ou `before` do histórico inválidos |
| `404` | A chamada não existe (ou é de outra instância) |
| `409` | O motor não está ativo na instância; a chamada não está em um estado que permita a ação (por exemplo atender uma que já não toca, ou `enable` sem vídeo); o histórico está desligado (`CALL_HISTORY`) |
| `429` | Limite de chamadas discadas por minuto, chamadas simultâneas demais (`CALL_MAX_CONCURRENT`) ou bilhetes de stream demais em aberto |
| `502` | O WhatsApp ou a biblioteca não conseguiu realizar a chamada |
| `500` | Falha ao avisar o outro lado ao desligar (a chamada termina do seu lado mesmo assim) |

---

## Rejeitar Chamada

Rejeita uma chamada recebida no WhatsApp. Vale **com ou sem** o motor de chamadas. Com o motor ligado, a rejeição passa por ele.

**Endpoint**: `POST /call/reject`

**Headers**:
```
Content-Type: application/json
apikey: SUA-CHAVE-API
```

**Body**:
```json
{
  "callCreator": "5511999999999@s.whatsapp.net",
  "callId": "ABC123XYZ"
}
```

| Campo | Tipo | Obrigatório | Descrição |
|-------|------|-------------|-----------|
| `callCreator` | string (JID) | ✅ Sim | JID de quem está ligando |
| `callId` | string | ✅ Sim | ID da chamada |

Os dados (`callCreator` e `callId`) chegam pelo webhook no evento `CallOffer`.

**Resposta de Sucesso (200)**:
```json
{ "message": "success" }
```

### Rejeição automática

1. Receba o `CallOffer` no seu webhook.
2. Pegue o `callId` e quem ligou.
3. Chame `POST /call/reject` logo: se demorar, a chamada pode cair antes.

Para rejeitar só algumas (fora do horário, de números fora de uma lista, ou só as de vídeo), aplique a regra no seu webhook antes de chamar o endpoint. A instância também tem `rejectCall` e `msgRejectCall` nas configurações avançadas para rejeitar tudo automaticamente.

> Sem o motor de chamadas (`callsEnabled`), **não** é possível atender pela API, só rejeitar. Com ele, use `POST /call/answer`.

---

## No painel (aba Chamadas)

No `/manager`, a instância ganhou a aba **Chamadas**: dá para atender, ligar e **conversar com o microfone e o alto-falante da própria página**, sem escrever código.

- **Chamadas agora**: as chamadas que o servidor acompanha na instância, atualizadas a cada 2 s, com a fase, o relógio, os contadores do stream e o aviso de "sem áudio do outro lado". Para cada uma: **Atender no navegador** (e **Rejeitar**) numa chamada que toca, **Falar pelo navegador** e **Desligar** nas demais.
- **Ligar**: o número com DDI e DDD; o telefone toca e o áudio é o da página. Com a chave **Videochamada** (aparece quando o navegador sabe codificar e decodificar vídeo) a chamada sai como videochamada e a câmera do computador é ligada.
- **Vídeo**: numa chamada com vídeo, a imagem do outro lado aparece na página, girada para ficar em pé, e a sua câmera aparece pequena no canto. O botão **Iniciar vídeo** (ou **Ligar câmera**/**Desligar câmera**) liga a sua câmera: numa chamada de áudio ele pede ao outro lado para passar a vídeo (`POST /call/video` com `start`), e quando o outro lado pede, o painel **aceita na hora** (`accept`): o iPhone mostra a câmera dele enquanto espera a resposta e retira o pedido em poucos segundos, voltando para voz (medido com o script de teste: aceito 0,5 s depois do pedido, o vídeo veio; aceito 6 s depois, o iPhone já tinha voltado e nenhuma imagem chegou), mais rápido do que uma pessoa lê o aviso e clica. A chave **Aceitar pedidos de vídeo na hora**, ligada por padrão, controla isso; desligada, aparece o botão **Aceitar vídeo**, que pode chegar tarde. Sem câmera no computador, o pedido é aceito do mesmo jeito e a chamada segue **recebendo** o vídeo do contato. Atender uma videochamada pelo painel liga a câmera; se ela for negada ou não existir, a chamada segue recebendo.
- **Histórico**: o [histórico de chamadas](#histórico-de-chamadas) com filtros por direção, resultado e contato, "Carregar mais" e **Apagar tudo**. Se o servidor não guarda histórico (`CALL_HISTORY`), a aba explica como ligar.
- **Comportamento**: o interruptor **Chamadas** (`callsEnabled`) agora está no painel. Vale na próxima conexão da instância.

O telefone do navegador usa o stream desta página: PCM 16 kHz em quadros binários, com os eventos de fala (o painel mostra "Falando" quando o outro lado fala) e a reprodução posicionada pelos `timestamp`, para que as pausas continuem pausas.

O **vídeo** é H.264 em Annex-B, como o stream o carrega, e o navegador faz a codificação e a decodificação com **WebCodecs** (o servidor só leva o vídeo, não converte). Recebido: o painel espera um keyframe antes de decodificar, tira o codec do SPS do keyframe e recomeça no próximo keyframe depois de uma perda; o navegador não pode pedir um keyframe ao WhatsApp, então uma perda pode congelar a imagem por alguns segundos. Enviado: a câmera é recortada para **360×640 em pé** (a tela da chamada no celular é em pé; uma imagem deitada aparece pequena, com faixas), codificada em H.264 baseline a 15 quadros por segundo e uns 500 kbit/s, com um keyframe a cada 2 s e sempre que o WhatsApp pede. O delimitador de acesso que o codificador do Chrome e do Edge põe no começo de cada imagem é retirado antes do envio. A câmera só envia quando a chamada pode carregar vídeo (uma videochamada, ou um upgrade que o outro lado aceitou). O stream é aberto **antes** de atender, para o áudio de uma chamada atendida sem stream não se perder. Se o microfone for negado, a chamada **não** é atendida.

**Requisitos e limites**
- **Fones de ouvido**: sem eles o alto-falante volta para o microfone e o outro lado se ouve de volta (o navegador cancela parte do eco, não todo).
- O navegador só libera o microfone em **página segura**: `https` ou `localhost`. Em `http` por um IP de rede o painel avisa.
- Se o painel é servido por um endereço diferente do da API, a origem da página precisa estar em `CALL_STREAM_ORIGINS` (o `Origin` do WebSocket é o da página).
- **Vídeo** exige um navegador com WebCodecs e H.264 (Chrome, Edge ou Safari recentes). Sem isso o painel avisa e a chamada segue só com áudio; sem câmera, a chamada segue recebendo vídeo, mas não envia.
- **Uma chamada por vez** no telefone do navegador; as outras aparecem na lista e podem ser atendidas pela API.
- Sair da página ou fechar a aba **solta o áudio**, e a chamada cai em `CALL_STREAM_GRACE` segundos.
- O áudio é capturado por um AudioWorklet que o build coloca num arquivo próprio (`manager/dist/assets/capture-worklet-*.js`): a política de segurança do painel (`script-src 'self'`) não aceitaria um worklet criado na hora.

Validado ao vivo (02/10/2026, navegador de desktop, número real e iPhone): atender uma chamada recebida pelo painel, silenciar e reativar o microfone, desligar, e ligar pelo card **Ligar**. O navegador manda áudio contínuo (13 s de conversa, 221 quadros de 60 ms no servidor).

**Vídeo, o que foi verificado:** receber a videochamada do iPhone e mostrá-la na página (143 e depois 240 imagens entregues, nenhuma descartada); atender e discar videochamadas; e **enviar vídeo**, testado com a câmera falsa do Edge (`--use-fake-device-for-media-stream`) porque o computador de teste não tem câmera: o iPhone mostrou a imagem de teste **em pé, preenchendo a tela**, e o servidor contou 258 imagens enviadas em 16 s (15 quadros por segundo pedidos), nenhuma descartada. O **pedido de vídeo do iPhone** numa chamada de voz foi aceito na hora pelo painel num computador sem câmera: o vídeo do iPhone chegou e ficou (107 imagens entregues, nenhuma descartada). **Não verificado:** uma câmera de verdade (só a falsa), o botão **Iniciar vídeo** numa chamada de voz, o botão manual **Aceitar vídeo** (com a chave desligada), ligar e desligar a câmera durante a chamada, e outros navegadores além do Edge.

---

## Teste ao vivo

O repositório traz um script que exercita tudo isso com uma chamada real: `docker/test-stack/call-stream-test.py` (precisa de `pip install websockets`). Ele atende (ou disca), grava o áudio recebido em WAV e o vídeo em `.h264` (mais um `.orient` com a rotação de cada quadro), devolve o áudio em eco e pode mandar um arquivo H.264 de teste. `--help` mostra as opções e o comando de `ffmpeg` para gerar o vídeo de teste.

```bash
python call-stream-test.py --apikey TOKEN --echo                     # espera uma chamada, atende e devolve o áudio
python call-stream-test.py --apikey TOKEN --dial 5511999990000 --tone 3
python call-stream-test.py --apikey TOKEN --dial 5511999990000 --video --video-in test.h264
```

Opções que exercitam o que há de mais novo: `--format` (`pcm16`, `pcm8`, `pcm24`, `mulaw`, `alaw`), `--binary` (quadros binários), `--speech` (imprime os `speech_start` e `speech_end`) e `--tone-burst` (enfileira o tom inteiro de uma vez e imprime quando o `mark` volta). A gravação em WAV é posicionada pelos `timestamp`, com o silêncio no lugar, e mantém a taxa do formato escolhido.

---

## Próximos Passos

- [Sistema de Eventos](../recursos-avancados/events-system.md) - Configurar webhooks
- [API de Instâncias](./api-instances.md) - `callsEnabled` nas configurações avançadas
- [Variáveis de Ambiente](../referencia/environment-variables.md)
- [Visão Geral da API](./api-overview.md)
