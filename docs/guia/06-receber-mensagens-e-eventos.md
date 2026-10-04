# 6. Receber mensagens e eventos

Mandar mensagem é a parte fácil. Para **saber o que acontece** (chegou mensagem, a conexão caiu, alguém ligou), o servidor avisa o seu sistema. Cada aviso é um **evento**.

## Como o aviso chega

Você escolhe um ou mais **canais**. Todos entregam os mesmos eventos:

| Canal | Como funciona | Quando escolher |
|---|---|---|
| **Webhook** | O servidor faz um `POST` com um JSON para um endereço seu. | O mais simples. Serve para quase tudo (n8n, Make, Zapier, um backend seu). |
| **WebSocket** | Seu sistema abre uma conexão aberta e recebe os eventos por ela. | Painéis e telas em tempo real. |
| **RabbitMQ** | O servidor publica os eventos numa fila AMQP. | Quando já existe um RabbitMQ e você quer entrega confiável. |
| **NATS** | O servidor publica em tópicos NATS. | Quando já existe um NATS e você quer baixa latência. |

Pode ligar mais de um ao mesmo tempo.

## Configurar um webhook pelo painel

1. Abra a instância → aba **Webhook e eventos**.
2. Em **Webhook**, cole o endereço do seu sistema (por exemplo `https://meusite.com/whatsapp`).
3. Em **Eventos**, marque só o que você vai usar. Quanto menos eventos, menos tráfego.
4. Salve. Vale na hora, sem reiniciar a instância.

Para **desligar** o webhook, apague o endereço e salve; o painel cuida de mandar o valor certo ao servidor.

Pela API o equivalente é `PUT /instance/{id}/integrations`.

## O que chega

Todo aviso tem o mesmo formato por fora:

```json
{
  "event": "MESSAGE",
  "instance": "loja",
  "data": { "...": "dados específicos do evento" }
}
```

Os exemplos completos de cada evento estão no [guia técnico de eventos](../wiki/recursos-avancados/events-system.md).

### Mudança importante em relação ao projeto original

Os avisos **não trazem mais o `instanceToken`** (o token é a senha da instância, e qualquer sistema que recebe os eventos poderia usá-la). Se uma integração antiga ainda lê esse campo, ligue `WEBHOOK_INCLUDE_TOKEN=true` no servidor.

## Os tipos de evento

| Grupo | Evento | Quando acontece |
|---|---|---|
| Mensagens | `MESSAGE` | Chegou uma mensagem (texto, foto, áudio, enquete, voto...). |
| | `SEND_MESSAGE` | Esta instância enviou uma mensagem. |
| | `READ_RECEIPT` | Uma mensagem foi entregue, lida ou reproduzida. |
| | `BUTTON_CLICK` | Alguém tocou num botão ou item de lista. |
| Presença e chats | `PRESENCE` | Um contato ficou online/offline (precisa assinar o contato). |
| | `CHAT_PRESENCE` | Digitando, gravando áudio; chat fixado, arquivado ou silenciado. |
| Sessão | `CONNECTION` | Conectou, desconectou, foi banido, falhas e avisos do sistema. |
| | `QRCODE` | Novo QR Code, pareamento concluído ou com erro, passkey. |
| | `CALL` | Ligação recebida, atendida, encerrada. |
| | `HISTORY_SYNC` | O celular mandou o histórico de conversas. |
| Contatos e grupos | `CONTACT`, `GROUP`, `NEWSLETTER`, `LABEL`, `PICTURE`, `USER_ABOUT` | Mudanças em contatos, grupos, canais, etiquetas, fotos de perfil e recados. |

Por padrão, eventos de **status** (stories) são ignorados, e os de grupos, não. Dá para mudar nos interruptores de **Comportamento** ou nas variáveis `EVENT_IGNORE_GROUP` e `EVENT_IGNORE_STATUS`.

## E se o meu sistema estiver fora do ar?

O WhatyGo criou uma **fila por destino** para não perder tudo nem derrubar o servidor:

- Se a entrega falha, o servidor **tenta de novo** algumas vezes, esperando cada vez mais (1 s, 5 s, 30 s, 2 min).
- Se o destino continua fora, ele é marcado como "degradado": os eventos seguintes recebem uma tentativa só, até um passar.
- A fila tem limite (1000 eventos ou 64 MB por destino). Estourou? O evento **mais antigo** é descartado, e isso fica contado.
- Dá para ver o estado das filas em `GET /instance/runtimes` e nas métricas.

Na prática: uma queda curta do seu sistema não perde mensagens, mas uma queda longa pode perder as mais antigas. Se isso é crítico, use RabbitMQ.

## Mídias (fotos, áudios, documentos)

Por padrão os avisos trazem o **link** da mídia (`WEBHOOK_FILES=true`). Para guardar arquivos num armazenamento próprio, o servidor integra com **MinIO / S3**. No WhatyGo, o bucket **não é mais público**: os arquivos são servidos por links temporários (7 dias por padrão). Detalhes em [Configuração e segurança](./08-configuracao-e-seguranca.md).

## Receber sem webhook: guardar no banco

`DATABASE_SAVE_MESSAGES=true` grava as mensagens no PostgreSQL. A tabela só cresce (não há limpeza automática), então só ligue se precisar.
