# 10. O que mudou no fork

O WhatyGo parte do **Evolution Go v0.7.2**. Este capítulo conta, sem jargão, o que já é diferente do original. A lista técnica completa, com números de PR e detalhes, está no [CHANGELOG](../../CHANGELOG.md), na [triagem](../../FORK-TRIAGE.md) e nas [propostas](../../FEATURE-PROPOSALS.md).

## Por que existe

O repositório público do original é um **espelho** de um repositório interno: recebe cópias automáticas (`sync: 0.7.x from main`) e os pedidos de correção da comunidade (cerca de 60 *issues* e 60 *pull requests* abertos na época) ficavam sem resposta. Este fork foi criado para **corrigir, melhorar e ajustar**, e a regra é: o que é feature nova passa por avaliação antes (`FEATURE-PROPOSALS.md`).

## Em resumo

| Área | Antes (original) | Agora (fork) |
|---|---|---|
| **Estabilidade** | O servidor inteiro caía com `concurrent map writes` ou `concurrent write to websocket`; vazava conexões do banco (`too many clients`); pânico em alguns eventos. | Corrigido e testado com verificação de corrida. |
| **Reconexão** | Pouco previsível; reconexões duplicadas; instâncias presas no meio da reconexão. | Uma execução por instância, reconexão com espera crescente, restauração ao iniciar. |
| **Biblioteca do WhatsApp** | whatsmeow de junho/2026. | Atualizada (setembro/2026, 72 commits) e Go 1.26. |
| **Erros da API** | `500` para quase tudo. | Status certo e um `code` estável (`503`, `409`, `429`, `404`...). |
| **Ativação de licença** | Exigia registro num serviço da Evolution Foundation (a API ficava em `503` até lá) e enviava um sinal periódico a ele. | Removida: o servidor funciona ao subir, sem registro e sem enviar nada a terceiros ([Instalação](./02-instalacao.md#não-há-ativação-de-licença)). |
| **Painel `/manager`** | Sem código-fonte no repositório; só o arquivo compilado. 12 de 16 eventos. | Refeito do zero, com código aberto na pasta `manager/`, tema claro/escuro, 16 eventos, teste de 12 tipos de envio, telefone no navegador. |
| **Botões e carrossel** | Erros `405`/`473`; carrossel sumia no iPhone. | Funcionam (iPhone e Web, conta Business). Lista vira botões de resposta. |
| **Chamadas** | Só rejeitar. | Atender, ligar, vídeo e áudio por WebSocket 🧪. |
| **Segurança** | Bucket MinIO público, token nos eventos, contêiner como administrador, chave de exemplo aceita. | Tudo isso foi fechado (veja [Segurança](./08-configuracao-e-seguranca.md)). |
| **Operação** | `/server/ok` e só. | `/health`, diagnóstico de runtime, `/metrics`, logs por instância. |
| **Envio** | Sem limite de simultâneos. | Fila com limite e `429` com `Retry-After`. |
| **Webhook** | Tentativas fixas, sem limite de fila. | Fila por destino, limites, espera crescente e estatísticas. |

## Novidades de API (resumo)

Veja a lista com a situação de cada uma em [Funcionalidades](./04-funcionalidades.md). As principais: `viewOnce`, `/send/pollVote`, `quoted.text`, `/message/subscribe` em lote, `/message/rerequest`, `/user/lid`, `/user/devices`, `/user/business`, `/user/contacts`, `/group/inviteinfo`, `/group/joininvite`, `/group/requests`, `/chat/disappearing`, `/call/*`, `/health`, `/metrics`, `/instance/{id}/runtime`, `/instance/{id}/integrations`, `/instance/proxy/{id}`.

## Mudanças que podem quebrar uma integração existente

Se você usava o original e vai trocar, leia:

1. **Os eventos não trazem `instanceToken`.** `WEBHOOK_INCLUDE_TOKEN=true` restaura.
2. **`POST /instance/disconnect` realmente desconecta** (antes reiniciava o cliente). A instância só volta com **Conectar**.
3. **Erros têm outro formato:** `{"error": "...", "code": "..."}` e o status HTTP diz o tipo de falha.
4. **Chave de exemplo é recusada** na inicialização.
5. **`WADEBUG` e `LOGTYPE`** são os nomes reais (antes o código lia `DEBUG_ENABLED`/`LOG_TYPE`).
6. **URLs de mídia só públicas** (`ALLOW_PRIVATE_URLS=true` para liberar).
7. **MinIO privado** (`MINIO_PUBLIC_BUCKET=true` volta ao antigo).
8. **Tamanho máximo das requisições** (4 MB; arquivos, 150 MB).
9. **Imagem Docker sem administrador** (uid 10001); o ponto de entrada ajusta volumes antigos.

## Atenção ao migrar do original para o fork

- O banco do WhatsApp (whatsmeow) passa da versão **14 para a 16**. As migrações **só andam para frente**: depois que o fork abrir o banco, a imagem do original **não consegue mais abri-lo**. **Faça backup do `whatygo_auth` antes.**
- A tabela de votos de enquete (`poll_votes`) muda de chave; voltar para a imagem antiga quebra o salvamento de votos (o resto continua).
- Novas colunas e índices são criados sozinhos na partida (por exemplo, `instances.calls_enabled`).

## Onde está a imagem pronta

`ghcr.io/lucasgiovannibr/whatygo` (público; antes do WhatyGo a imagem se chamava `ghcr.io/lucasgiovannibr/evolution-go`, que não recebe mais atualizações). Etiquetas: `latest` (a mais recente da `main`), um número de versão (`0.7.2`), e `sha-...` para uma versão exata.

## O que **não** mudou

- A API continua compatível: as rotas do original seguem existindo (as novas são adições).
- A licença do código (Apache 2.0) e os créditos ao projeto original ([Avisos legais](./11-avisos-legais-e-creditos.md)).

## O que ainda não foi feito (de propósito)

Está em aberto para decisão: thumbnails grandes em links, encaminhar mensagens, evento de agenda, gravação de chamadas (**decidido que não**), chamadas em grupo, chamadas por proxy, WebRTC de verdade, criptografia do proxy no banco. A lista com motivos está em [FEATURE-PROPOSALS.md](../../FEATURE-PROPOSALS.md).
