# 8. Configuração e segurança

A configuração é feita por **variáveis de ambiente**: linhas `NOME=valor` no arquivo `.env` (e repassadas ao servidor pelo `docker-compose.yml`). Depois de mudar, aplique com:

```bash
docker compose up -d
```

A lista completa, com todos os padrões, está em [Variáveis de ambiente](../wiki/referencia/environment-variables.md). Aqui ficam as que importam para a maioria das pessoas.

## O essencial

| Variável | Para que serve | Padrão |
|---|---|---|
| `GLOBAL_API_KEY` | Chave de administrador. **Obrigatória.** Mínimo recomendado: 32 caracteres aleatórios. | — |
| `POSTGRES_AUTH_DB`, `POSTGRES_USERS_DB` | Endereços dos dois bancos. O arquivo de instalação simples já preenche. | — |
| `SERVER_PORT` | Porta dentro do contêiner. | `4000` (na imagem) |
| `CONNECT_ON_STARTUP` | Reconectar os números sozinhos ao iniciar. | `false` (a instalação simples liga) |
| `DATABASE_SAVE_MESSAGES` | Guardar as mensagens no banco. | `false` |
| `CLIENT_NAME` | Nome do servidor. Só serve para separar quais instâncias cada servidor inicia quando há mais de um. | `whatygo` |

## Comportamento do WhatsApp

| Variável | Para que serve | Padrão |
|---|---|---|
| `QRCODE_MAX_COUNT` | Quantos QR Codes gerar antes de a instância parar. Parada, ela só volta ao conectar de novo (botão conectar ou abrir o QR no painel). `0` nunca para. | `5` |
| `CHECK_USER_EXISTS` | Conferir se o número tem WhatsApp antes de enviar. | `true` |
| `EVENT_IGNORE_GROUP` / `EVENT_IGNORE_STATUS` | Ignorar eventos de grupos / de status. | `false` / `true` |
| `DISAPPEARING_AUTO_APPLY` | Aplicar o tempo das mensagens temporárias ao enviar. | ligado |
| `OS_NAME` | Nome do "aparelho" que aparece em *Aparelhos conectados* no celular. | `Linux` |

## Limites de envio (proteção do seu número)

| Variável | Para que serve | Padrão |
|---|---|---|
| `SEND_MAX_CONCURRENT` | Envios simultâneos por instância. | `4` |
| `SEND_RATE_PER_MIN` | Teto de envios por minuto (por instância). | desligado |
| `SEND_QUEUE_WAIT_SEC` | Quanto um envio espera na fila antes de responder `429`. | `30` |

## Logs

| Variável | Para que serve |
|---|---|
| `LOGTYPE` | `console` (na tela, para `docker compose logs`) ou arquivo. |
| `WADEBUG` | Nível de detalhe da biblioteca do WhatsApp (`INFO`, `DEBUG`). |
| `LOG_LEVEL` | Nível dos logs do servidor. Linhas de depuração só saem com `debug`. |

> Os nomes antigos `DEBUG_ENABLED` e `LOG_TYPE` ainda funcionam como reserva, mas os nomes acima são os corretos.

## Cuidados para não ser banido

O WhatsApp não tem regra pública de limites, mas a experiência da comunidade é:

- **Use um chip dedicado** e aqueça aos poucos: comece com ~100 mensagens por dia e suba devagar.
- **Não mande a quem não conhece você.** Mensagem repetida e em massa para números desconhecidos é o jeito mais rápido de perder a conta.
- **Varie o texto** e deixe o contato **sair da lista** (responder "SAIR").
- **Espere entre mensagens** (2 a 5 segundos); os limites acima ajudam.
- **Não use "Sempre online"** sem necessidade: o celular deixa de notificar.
- Se aparecer um erro **463** ao enviar, o WhatsApp restringiu o alcance da conta por um tempo; o WhatyGo explica o motivo e até quando, quando o WhatsApp informa. Pare de enviar até passar.

## Segurança do servidor

### O que o WhatyGo já faz por você

- **Recusa chaves de exemplo** (`change-me`, `sua-chave-api-segura-aqui`...). Uma chave de menos de 32 caracteres gera um aviso.
- **Contêiner sem privilégios de administrador**, com verificação de saúde.
- **Links de mídia só para endereços públicos.** Quem pede ao servidor para baixar uma imagem não consegue apontá-lo para a sua rede interna ou para os dados de nuvem. `ALLOW_PRIVATE_URLS=true` libera, só se você precisa e entende o risco. (Webhooks para a rede interna são sempre permitidos.)
- **Tamanho máximo de requisição** (`MAX_BODY_MB` 4; arquivos: `MAX_MEDIA_BODY_MB` 150).
- **CORS** pelas origens de `CORS_ORIGINS` (vazio ou `*` aceita todas).
- **MinIO privado:** mídias por links temporários (`MINIO_URL_TTL_HOURS`, máximo 168 h).
- **Senha do proxy** e **token da instância** não saem mais pela API e pelos eventos.
- **Freio para quem tenta adivinhar chaves:** depois de `AUTH_FAIL_LIMIT` (30) autenticações erradas num minuto, aquele IP recebe `429` até o minuto passar. Só as falhas contam. O token de uma instância nova precisa de pelo menos 16 caracteres (se você não informar, o servidor gera um).
- **O IP do cliente não é forjável:** o `X-Forwarded-For` só vale vindo de quem está em `TRUSTED_PROXIES`.
- **Mídia recebida tem teto** (`MAX_RECEIVED_MEDIA_MB`, 50): arquivos maiores não são baixados (o aviso chega com `mediaSkipped`). Quem envia decide o tamanho do arquivo, e um documento de 2 GB derrubaria o servidor.
- **WebSocket `/ws`** limita o que o assinante envia e desconecta o que não acompanha os eventos.

### O que cabe a você

1. **Guarde a `GLOBAL_API_KEY`** como uma senha de banco: quem a tem controla tudo.
2. **Coloque HTTPS** na frente (Caddy, Nginx Proxy Manager, Traefik...). Sem HTTPS, as chaves viajam em texto puro.
3. **Feche as portas.** Publique só 80/443 (e SSH). O banco de dados **não** deve ficar na internet. Os arquivos de Compose do projeto já prendem tudo em `127.0.0.1` (para abrir a API direto, `BIND_ADDRESS=0.0.0.0` no `.env`).
   **Atrás de um proxy reverso**, ponha o endereço dele em `TRUSTED_PROXIES` (por exemplo `172.18.0.0/16`): sem isso todos os clientes aparecem com o IP do proxy e o limite de tentativas erradas vale para todos juntos. `HTTP_PROXY`/`HTTPS_PROXY` do ambiente **não** são usados para baixar mídia nem enviar webhooks (`OUTBOUND_PROXY_FROM_ENV=true` muda isso).
4. **Faça backup** dos bancos `whatygo_auth` e `whatygo_users` (comando em [Instalação](./02-instalacao.md#atualizar-para-uma-versão-nova)). O `whatygo_auth` guarda as sessões dos números: perdê-lo significa ler todos os QR Codes de novo.
5. **Atualize** com regularidade.
6. **Não exponha** `/debug/pprof` (`ENABLE_PPROF`) nem `/metrics` na internet.

### Telemetria

O WhatyGo **não envia telemetria nem dados de uso** a ninguém. O envio que o Evolution Go fazia ao serviço de licença dele (registro, sinal periódico e contagem de mensagens) foi removido. O servidor só conversa com o WhatsApp e com os destinos que você configura (webhooks, filas, MinIO/S3 e proxy).

## Memória

Se o contêiner tem limite de memória (`MEM_LIMIT=1g` no `.env` da instalação simples), o servidor ajusta o coletor de lixo do Go a 80 % dele (`MEMORY_LIMIT_RATIO`) para não ser morto por falta de memória numa rajada de mídia. Os logs de cada mensagem ficam no nível `debug` (`LOG_LEVEL=debug` para vê-los).

## Vários servidores (avançado)

Com `INSTANCE_LOCK` (ligado por padrão), o banco impede que dois servidores usem o mesmo número ao mesmo tempo, o que derrubaria a sessão. Use `CLIENT_NAME` diferente em cada servidor para dividir quais números cada um inicia.
