# Variáveis de Ambiente

Referência rápida de variáveis de ambiente do WhatyGo.

Para documentação detalhada, consulte: [Configuração](../fundamentos/configuration.md)

---

## Obrigatórias

| Variável | Descrição | Exemplo |
|----------|-----------|---------|
| `GLOBAL_API_KEY` | Chave de autenticação da API | `df16caad-d0d2-41b2-bec5-75b90048a0db` |
| `DATABASE_SAVE_MESSAGES` | Salvar mensagens no banco | `false` |

---

## Servidor

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `SERVER_PORT` | `4000` | Porta HTTP |
| `CLIENT_NAME` | `whatygo` | Nome identificador |
| `OS_NAME` | `Linux` | Sistema operacional |

---

## Banco de Dados

| Variável | Descrição |
|----------|-----------|
| `POSTGRES_AUTH_DB` | Connection string banco de autenticação |
| `POSTGRES_USERS_DB` | Connection string banco de usuários |

**Formato:**
```env
POSTGRES_AUTH_DB=postgresql://user:pass@host:5432/whatygo_auth?sslmode=disable
POSTGRES_USERS_DB=postgresql://user:pass@host:5432/whatygo_users?sslmode=disable
```

---

## Logs

| Variável | Padrão | Valores | Descrição |
|----------|--------|---------|-----------|
| `WADEBUG` | `INFO` | `DEBUG`, `INFO`, `WARN`, `ERROR` | Nível de log do cliente WhatsApp (whatsmeow) |
| `LOGTYPE` | `console` | `console`, `file` | Destino de saída |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` | Nível mínimo dos logs das instâncias e do console (linhas de debug só com `debug`) |
| `LOG_KEEP_DELETED` | `false` | `true`/`false` | Manter o diretório de logs de uma instância depois de apagada (por padrão é removido: guarda token, JIDs e metadados) |
| `LOG_DIRECTORY` | `/app/logs` | - | Diretório de arquivos de log |
| `LOG_MAX_SIZE` | `100` | - | Tamanho máximo por arquivo (MB) |
| `LOG_MAX_BACKUPS` | `5` | - | Arquivos de backup a manter |
| `LOG_MAX_AGE` | `30` | - | Retenção em dias |
| `LOG_COMPRESS` | `true` | `true`/`false` | Compressão de logs antigos |

> `DEBUG_ENABLED` e `LOG_TYPE` (nomes que o código lia antes) continuam aceitos como alternativa a `WADEBUG` e `LOGTYPE`.

---

## Conexão e Comportamento

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `CONNECT_ON_STARTUP` | `false` | Conectar instâncias ao iniciar servidor |
| `WEBHOOK_FILES` | `true` | Enviar URLs de mídia em webhooks |
| `QRCODE_MAX_COUNT` | `5` | Tentativas máximas de QR Code |
| `CHECK_USER_EXISTS` | `true` | Validar destinatário antes de enviar |
| `CHECK_USER_CACHE_TTL_MIN` | `720` | Minutos que "este número está no WhatsApp" é lembrado antes de perguntar de novo (`0` pergunta a cada envio; "não registrado" é lembrado por 5 min) |
| `WEBHOOK_INCLUDE_TOKEN` | `false` | Inclui `instanceToken` no payload dos eventos. Desligado: o token é a chave de API da instância |
| `STARTUP_STAGGER_MS` | `300` | Pausa entre duas instâncias iniciadas por `CONNECT_ON_STARTUP` (mais até 50 % de jitter; `0` inicia todas de uma vez) |
| `RECONNECT_BACKOFF_BASE_SEC` | `5` | Espera antes da 2ª reconexão automática seguida; dobra a cada tentativa |
| `RECONNECT_BACKOFF_MAX_SEC` | `300` | Teto da espera entre reconexões automáticas (o contador zera depois de 60 s conectado) |
| `INSTANCE_LOCK` | ligado | Uma réplica por instância (locks consultivos do Postgres). `false` desliga |

---

## Eventos

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `EVENT_IGNORE_GROUP` | `false` | Ignorar eventos de grupos |
| `EVENT_IGNORE_STATUS` | `true` | Ignorar eventos de status/stories |
| `WEBHOOK_URL` | - | URL para callbacks HTTP |

---

## RabbitMQ (AMQP)

| Variável | Descrição |
|----------|-----------|
| `AMQP_URL` | URL de conexão RabbitMQ |
| `AMQP_GLOBAL_ENABLED` | Habilitar filas globais |
| `AMQP_GLOBAL_EVENTS` | Eventos a publicar (separados por vírgula) |
| `AMQP_SPECIFIC_EVENTS` | Eventos específicos por instância |

**Exemplo:**
```env
AMQP_URL=amqp://user:pass@rabbitmq:5672/vhost
AMQP_GLOBAL_ENABLED=true
AMQP_GLOBAL_EVENTS=messages.upsert,messages.update,connection.update
```

---

## NATS

| Variável | Descrição |
|----------|-----------|
| `NATS_URL` | URL de conexão NATS |
| `NATS_GLOBAL_ENABLED` | Habilitar publicação global |
| `NATS_GLOBAL_EVENTS` | Eventos a publicar |

**Exemplo:**
```env
NATS_URL=nats://nats:4222
NATS_GLOBAL_ENABLED=true
NATS_GLOBAL_EVENTS=messages.upsert,connection.update
```

---

## MinIO/S3

| Variável | Descrição |
|----------|-----------|
| `MINIO_ENABLED` | Habilitar armazenamento S3-compatible |
| `MINIO_ENDPOINT` | Endpoint do servidor |
| `MINIO_ACCESS_KEY` | Access Key para autenticação |
| `MINIO_SECRET_KEY` | Secret Key para autenticação |
| `MINIO_BUCKET` | Nome do bucket |
| `MINIO_USE_SSL` | Utilizar HTTPS |
| `MINIO_REGION` | Região do bucket (AWS) |
| `MINIO_PUBLIC_BUCKET` | `true` torna **todos** os objetos do bucket públicos (substitui a policy do bucket). Padrão: desligado; a mídia é servida por URLs pré-assinadas |
| `MINIO_URL_TTL_HOURS` | Validade das URLs pré-assinadas, em horas (padrão e máximo `168`) |

A mídia é gravada em `whatygo-medias/<instanceId>/` e apagada junto com a instância. O bucket é criado se não existir.

**Exemplo:**
```env
MINIO_ENABLED=true
MINIO_ENDPOINT=localhost:9000
MINIO_ACCESS_KEY=minioadmin
MINIO_SECRET_KEY=minioadmin
MINIO_BUCKET=whatygo-media
MINIO_USE_SSL=false
MINIO_REGION=us-east-1
```

---

## Proxy HTTP

| Variável | Descrição |
|----------|-----------|
| `PROXY_HOST` | Hostname do proxy |
| `PROXY_PORT` | Porta do proxy |
| `PROXY_USERNAME` | Usuário (opcional) |
| `PROXY_PASSWORD` | Senha (opcional) |

**Exemplo:**
```env
PROXY_HOST=proxy.empresa.com
PROXY_PORT=8080
PROXY_USERNAME=usuario
PROXY_PASSWORD=senha
```

---

## Chamadas

Só têm efeito nas instâncias com `callsEnabled` ligado. Valores inválidos ou não positivos voltam ao padrão (menos onde o `0` é a escolha: `CALL_MEDIA_STALL`, `CALL_MAX_DURATION`, `CALL_SILENCE_TIMEOUT` e `CALL_HISTORY_RETENTION_DAYS`). Detalhes em [API de Chamadas](../guias-api/api-call.md).

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `CALL_MAX_CONCURRENT` | `4` | Chamadas simultâneas por instância (as recebidas além disso são rejeitadas) |
| `CALL_RING_TIMEOUT` | `90` | Segundos até largar uma chamada que ninguém atendeu |
| `CALL_STREAM_GRACE` | `10` | Segundos que uma chamada atendida espera o stream voltar antes de ser desligada |
| `CALL_DIAL_LIMIT` | `6` | Chamadas discadas por minuto por instância |
| `CALL_STREAM_ORIGINS` | - | Origens de navegador aceitas no WebSocket do stream, separadas por vírgula (`*` aceita todas) |
| `CALL_MEDIA_STALL` | `15` | Segundos que uma chamada ativa com stream pode ficar sem áudio do outro lado antes do evento `CallMediaStalled`; `0` desliga |
| `CALL_MEDIA_STALL_HANGUP` | `false` | Com `true`, uma chamada nessa situação também é desligada (`media_stalled`) |
| `CALL_MAX_DURATION` | `0` | Segundos que uma chamada atendida pode durar, contados da mídia pronta; `0` é sem limite (`max_duration`) |
| `CALL_SILENCE_TIMEOUT` | `0` | Segundos sem barulho de nenhum dos dois lados antes de desligar uma chamada com stream; `0` nunca (`silence_timeout`) |
| `CALL_HISTORY` | `false` | Guarda o histórico de chamadas (só metadados) na tabela `call_records`, com `GET`/`DELETE /call/history` |
| `CALL_HISTORY_RETENTION_DAYS` | `90` | Dias que o histórico é guardado; `0` guarda para sempre |

---

## Recursos Adicionais

| Variável | Descrição |
|----------|-----------|
| `API_AUDIO_CONVERTER` | URL de serviço de conversão de áudio |
| `API_AUDIO_CONVERTER_KEY` | Chave de autenticação do conversor |

---

## Versão WhatsApp (Avançado)

| Variável | Descrição |
|----------|-----------|
| `WHATSAPP_VERSION_MAJOR` | Versão major do WhatsApp Web |
| `WHATSAPP_VERSION_MINOR` | Versão minor do WhatsApp Web |
| `WHATSAPP_VERSION_PATCH` | Versão patch do WhatsApp Web |

**⚠️ Atenção**: Modificar versão do WhatsApp pode resultar em bloqueio. Deixar vazio para usar versão automática.

---

## Segurança e Limites

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `ALLOW_INSECURE_API_KEY` | `false` | O servidor se recusa a iniciar com uma `GLOBAL_API_KEY` publicada (as dos exemplos, `change-me`...). `true` inicia mesmo assim, só para desenvolvimento local |
| `ALLOW_PRIVATE_URLS` | `false` | URLs recebidas em requisições (mídia, figurinhas, preview de link, status) só podem apontar para endereços públicos; `true` libera redes privadas. Loopback e o endpoint de metadados da nuvem continuam bloqueados. Webhooks podem sempre apontar para a rede interna |
| `CORS_ORIGINS` | vazio | Origens de navegador autorizadas, separadas por vírgula. Vazio ou `*` libera todas |
| `MAX_BODY_MB` | `4` | Tamanho máximo do corpo de uma requisição |
| `MAX_MEDIA_BODY_MB` | `150` | Tamanho máximo nas rotas que recebem arquivo |
| `MAX_IMAGE_MEGAPIXELS` | `50` | Imagens que declaram mais pixels que isso são recusadas antes de decodificar |
| `MAX_CONCURRENT_CONVERSIONS` | `max(2, CPUs/2)` | Conversões simultâneas de ffmpeg/pdftoppm (cada uma tem timeout e teto de saída) |

---

## Desempenho e Filas

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `SEND_MAX_CONCURRENT` | `4` | Envios de uma mesma instância dentro da chamada de rede ao mesmo tempo; os demais esperam a vez (`0` desliga) |
| `SEND_RATE_PER_MIN` | desligado | Taxa sustentada de mensagens por minuto, por instância (rajada de ~6 s) |
| `SEND_QUEUE_WAIT_SEC` | `30` | Quanto um envio espera pela vez antes de receber `429` com `Retry-After` |
| `MEDIA_WORKERS` | `4` | Mensagens recebidas com mídia processadas ao mesmo tempo (download, conversão, upload), no processo todo |
| `MEDIA_WORKERS_PER_INSTANCE` | `2` | Parte de uma instância nesse limite |
| `MEDIA_ORDERED` | `false` | `true` processa a mídia dentro do handler, na ordem exata de chegada |
| `WEBHOOK_QUEUE_MAX_EVENTS` | `1000` | Eventos por destino na fila de webhook (acima disso o mais antigo é descartado) |
| `WEBHOOK_QUEUE_MAX_MB` | `64` | Bytes por destino na fila de webhook |
| `WEBHOOK_QUEUE_WORKERS` | `4` | Entregas simultâneas por destino (`1` entrega em ordem estrita) |

---

## Pool de Conexões do Banco

| Variável | Padrão | Descrição |
|----------|--------|-----------|
| `DB_MAX_OPEN_CONNS` | `25` | Conexões abertas por banco |
| `DB_MAX_IDLE_CONNS` | `10` | Conexões ociosas mantidas (nunca acima do limite anterior) |
| `DB_CONN_MAX_LIFETIME_MIN` | `5` | Minutos até renovar uma conexão |
| `DB_CONN_MAX_IDLE_MIN` | `1` | Minutos até fechar uma conexão ociosa |

Com `INSTANCE_LOCK` ligado, uma conexão do banco de usuários fica reservada para os locks.

---

## Exemplo Completo

```env
# Obrigatórias
GLOBAL_API_KEY=df16caad-d0d2-41b2-bec5-75b90048a0db
DATABASE_SAVE_MESSAGES=false

# Servidor
SERVER_PORT=4000
CLIENT_NAME=whatygo
OS_NAME=Linux

# Banco de Dados
POSTGRES_AUTH_DB=postgresql://postgres:senha@postgres:5432/whatygo_auth?sslmode=disable
POSTGRES_USERS_DB=postgresql://postgres:senha@postgres:5432/whatygo_users?sslmode=disable

# Logs
WADEBUG=INFO
LOGTYPE=console

# Comportamento
CONNECT_ON_STARTUP=false
WEBHOOK_FILES=true
CHECK_USER_EXISTS=true
EVENT_IGNORE_STATUS=true

# Webhook
WEBHOOK_URL=https://seu-servidor.com/webhook

# RabbitMQ (opcional)
AMQP_URL=amqp://admin:admin@rabbitmq:5672/default
AMQP_GLOBAL_ENABLED=true
AMQP_GLOBAL_EVENTS=messages.upsert,connection.update

# MinIO (opcional)
MINIO_ENABLED=true
MINIO_ENDPOINT=minio:9000
MINIO_ACCESS_KEY=minioadmin
MINIO_SECRET_KEY=minioadmin
MINIO_BUCKET=whatygo-media
MINIO_USE_SSL=false
```

---

## Recursos

- **[Configuração Detalhada](../fundamentos/configuration.md)** - Documentação completa de cada variável
- **[.env.example](https://github.com/lucasgiovannibr/whatygo/blob/main/docker/examples/.env.example)** - Arquivo de exemplo com todas as variáveis

---

**Documentação WhatyGo v1.0**
