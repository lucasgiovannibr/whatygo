# 4. Funcionalidades

Tudo o que o WhatyGo faz, organizado por assunto. Em cada tabela, a coluna **Situação** usa as marcas explicadas no [índice](./README.md#como-ler-as-marcas-de-maturidade): ✅ testado com número real, 🟡 parcial, 🧪 experimental, ⬜ herdado do projeto original e não retestado.

**Novo no WhatyGo** aparece como 🆕. As rotas citadas (`/send/text` etc.) são os endereços da API, para quem programa; quem usa só o painel pode ignorá-las.

## Índice

1. [Números (instâncias) e conexão](#1-números-instâncias-e-conexão)
2. [Enviar mensagens](#2-enviar-mensagens)
3. [Mensagens interativas (botões, lista, carrossel)](#3-mensagens-interativas)
4. [Agir sobre mensagens](#4-agir-sobre-mensagens)
5. [Conversas (chats)](#5-conversas-chats)
6. [Contatos e perfil](#6-contatos-e-perfil)
7. [Grupos, comunidades, canais e etiquetas](#7-grupos-comunidades-canais-e-etiquetas)
8. [Receber eventos](#8-receber-eventos)
9. [Chamadas de voz e vídeo](#9-chamadas-de-voz-e-vídeo)
10. [Painel web](#10-painel-web)
11. [Operação: saúde, diagnóstico e métricas](#11-operação-saúde-diagnóstico-e-métricas)
12. [Armazenamento e banco de dados](#12-armazenamento-e-banco-de-dados)
13. [Segurança](#13-segurança)

---

## 1. Números (instâncias) e conexão

| Funcionalidade | O que faz | Situação |
|---|---|---|
| **Várias instâncias** | Vários números no mesmo servidor, cada um isolado. | ✅ |
| Criar, listar, ver e apagar instâncias | Gestão do ciclo de vida (`/instance/create`, `/all`, `/info`, `/delete`). | ✅ |
| **Conectar por QR Code** | Liga o número lendo o QR Code. | ✅ |
| **Conectar por código de telefone** | Em vez do QR, digita-se um código de 8 caracteres no celular (`/instance/pair`). | ⬜ |
| **Chave de acesso (passkey)** | Para contas que exigem biometria/PIN; extensão `passkey-helper` melhorada para funcionar com 1Password/Bitwarden. | 🟡 🆕 |
| Desconectar, reconectar, sair (logout) | `/instance/disconnect`, `/reconnect`, `/logout`. O **desconectar agora desconecta de verdade** e a instância não volta sozinha até você conectar. | ✅ 🆕 |
| **Reconexão automática** | Se a conexão cai, tenta de novo com espera crescente (5 s, depois dobrando até 5 min). | ✅ 🆕 |
| **Reconectar tudo ao iniciar** | `CONNECT_ON_STARTUP`: números voltam sozinhos depois de reiniciar o servidor, um de cada vez. | ✅ |
| Forçar reconexão e ver logs | `/instance/forcereconnect/{id}`, `/instance/logs/{id}`. | ⬜ |
| **Proxy por instância** | Faz o número sair por um proxy (HTTP ou SOCKS5). Senha nunca é devolvida pela API. `PROXY_FAIL_CLOSED` impede que caia para conexão direta se o proxy falhar. | 🟡 🆕 (proxy real não foi testado) |
| **Configurações avançadas** | Sempre online, rejeitar chamadas, marcar como lidas, ignorar grupos/status, chamadas (`/advanced-settings`). Alterações são parciais: não apagam o resto. | ✅ 🆕 |
| **Integrações sem conectar** | Gravar webhook, eventos e filas sem iniciar a instância (`PUT /instance/{id}/integrations`). | ✅ 🆕 |
| **Uma réplica por instância** | Trava no banco que impede dois servidores de usarem o mesmo número ao mesmo tempo (o que derrubaria a sessão). | ✅ 🆕 |

## 2. Enviar mensagens

| Tipo | Detalhes | Situação |
|---|---|---|
| **Texto** | Com formatação do WhatsApp, menção de pessoas (ou de todos num grupo) e resposta a outra mensagem. | ✅ |
| **Responder citando** | Campo `quoted`; 🆕 o `quoted.text` agora preenche o texto do cartão da citação (antes aparecia vazio). | ✅ 🆕 |
| **Link com pré-visualização** | Título, descrição e imagem do cartão. | ⬜ |
| **Imagem, vídeo, áudio, documento** | Por link ou por envio de arquivo. Áudio vai como mensagem de voz. | ✅ |
| **"Ver uma vez"** | `viewOnce` em imagem, vídeo e áudio. | ✅ 🆕 |
| **Figurinha (sticker)** | Inclusive animada (correção do WhatyGo). | 🟡 🆕 |
| **Localização** | Nome, endereço e coordenadas. | ⬜ |
| **Contato (vCard)** | Cartão de contato. | ⬜ |
| **Enquete** | Com várias opções; resultado em `/polls/{id}/results`. Os votos de quem aparece só por LID passam a guardar o telefone real. | ✅ |
| **Votar em enquete** | `POST /send/pollVote`: o próprio número vota. | ✅ 🆕 |
| **Status (stories)** | Texto ou mídia no status do número. | ⬜ |
| **Mensagens temporárias** | O servidor **aprende** o tempo de apagar de cada conversa e o aplica ao enviar (`DISAPPEARING_AUTO_APPLY`); dá para mudar por conversa e definir o padrão. | ✅ 🆕 |
| **Respeito aos limites** | Fila de envio com limite de simultâneos, e limite por minuto opcional; responde `429` com `Retry-After` em vez de travar. | ✅ 🆕 |
| **Verificação do destinatário** | `CHECK_USER_EXISTS` confere se o número tem WhatsApp antes de enviar (com cache). | ⬜ |

Mídias por URL só aceitam endereços **públicos** (não aceitam rede interna), por segurança.

## 3. Mensagens interativas

Verificado em aparelho (iPhone e WhatsApp Web) com **conta WhatsApp Business** conectada como aparelho vinculado:

| Tipo | Situação | Observação |
|---|---|---|
| **Botões**: resposta rápida, copiar, abrir link, ligar; até 3 de resposta; combinados | ✅ 🆕 | Os erros 405/473 do original vinham do formato antigo, corrigido. Resposta + link juntos aparecem no celular, mas **não no WhatsApp Web**. |
| **Carrossel** de cartões com botões | ✅ 🆕 | Correção: campos vazios derrubavam a mensagem no iPhone. |
| **Pix** (botão de pagamento) | ✅ | Só no celular. |
| **Lista** | 🟡 | O WhatsApp **recusa listas** vindas de um aparelho vinculado. O servidor reenvia como **botões de resposta** (3 por mensagem). Quem toca gera o mesmo evento `BUTTON_CLICK`. |

> **Aviso:** um `200 OK` só diz que o servidor aceitou. Quem decide se a mensagem aparece é o WhatsApp, e ele às vezes descarta sem avisar. Sempre confira no aparelho. **Android e grupos não foram testados.**

## 4. Agir sobre mensagens

| Funcionalidade | Situação |
|---|---|
| **Reagir** com emoji (`/message/react`) | ⬜ |
| **Apagar** para todos e **editar** (`/message/delete`, `/edit`) | 🟡 🆕 (corrigido para funcionar com todos os formatos de número) |
| **Marcar como lida / reproduzida** | ⬜ |
| **Baixar mídia** recebida (`/message/downloadmedia`) | ⬜ |
| **Mostrar "digitando" / "gravando"** (`/message/presence`) | ⬜ |
| **Acompanhar um contato** (`/message/subscribe`): passa a receber online/offline/visto por último. Aceita até 100 números e diz o resultado de cada um. | 🟡 🆕 |
| **Pedir de novo uma mensagem que não chegou** (`/message/rerequest`) e evento `UndecryptableMessage` | 🟡 🆕 |

## 5. Conversas (chats)

| Funcionalidade | Situação |
|---|---|
| **Fixar / desafixar**, **arquivar / desarquivar** | ✅ 🆕 (estavam quebrados com certos formatos de número) |
| **Silenciar / reativar**: `duration` aceita `8h`, `1w`, `30m` ou `always` | ✅ 🆕 |
| **Mensagens temporárias** por conversa (`/chat/disappearing`) | ✅ 🆕 |
| **Pedir histórico** ao celular (`/chat/history-sync`) | ⬜ |

## 6. Contatos e perfil

| Funcionalidade | Situação |
|---|---|
| **Consultar** dados e foto de perfil de números (`/user/info`, com a **URL da foto** 🆕 e limite de tempo para não travar) | ✅ 🆕 |
| **Conferir** se números têm WhatsApp (`/user/check`) | ⬜ |
| **Listar e salvar contatos** (`/user/contacts`; 🆕 salvar). *Não existe remover contato pela API: o WhatsApp não permite.* | ✅ 🆕 |
| **Privacidade**: ver e alterar (`/user/privacy`); ver a privacidade do status (🆕) | ⬜ / ✅ 🆕 (status) |
| **Bloquear, desbloquear, ver bloqueados** | ⬜ (o evento de bloqueio foi visto ao vivo ✅) |
| **Mudar** foto, nome e recado (status) do perfil | ⬜ |
| **LID → telefone** (`/user/lid`): o WhatsApp esconde o telefone atrás de um código; isto traduz. | ✅ 🆕 |
| **Aparelhos** de um número (`/user/devices`) | ✅ 🆕 |
| **Perfil comercial** (`/user/business`) | 🟡 🆕 |
| **Timer padrão** de mensagens temporárias (`/user/defaultDisappearing`) | ✅ 🆕 |

## 7. Grupos, comunidades, canais e etiquetas

| Área | O que faz | Situação |
|---|---|---|
| **Grupos** | Criar, listar (`/group/list`, `/myall`), ver informações, nome, descrição, foto, link de convite, sair, ajustes, adicionar/remover/promover participantes. | ⬜ |
| Resultado por participante | `/group/participant` agora diz **quem entrou e quem falhou** (antes dizia "sucesso" mesmo sem adicionar). | ✅ 🆕 |
| **Entrar por convite** | Consultar um link ou cartão de convite **sem entrar** (`/group/inviteinfo`) e entrar (`/group/joininvite`). | ✅ (consulta) / 🟡 (entrar) 🆕 |
| **Pedidos para entrar** | Listar e aprovar/rejeitar (`/group/requests`, `/requests/update`). | 🟡 🆕 |
| **Comunidades** | Criar e adicionar/remover grupos (`/community/*`). | ⬜ |
| **Canais** (newsletters) | Criar, listar, ver, seguir, deixar de seguir, silenciar, marcar como visto, reagir. | 🟡 🆕 (as ações não foram testadas: não havia canal de teste) |
| **Etiquetas** | Etiquetar e desetiquetar conversas e mensagens, editar e listar (`/label/*`, `/unlabel/*`) — para contas Business. | ⬜ |

## 8. Receber eventos

Resumo (detalhes em [Receber mensagens e eventos](./06-receber-mensagens-e-eventos.md)):

| Funcionalidade | Situação |
|---|---|
| Canais: **webhook**, **WebSocket**, **RabbitMQ**, **NATS** | ✅ |
| **16 tipos de evento** escolhíveis por instância | ✅ 🆕 (o painel antigo oferecia só 12) |
| **Vários assinantes WebSocket** por instância, com escrita segura (corrige quedas do servidor) | 🟡 🆕 (testes automáticos com verificação de corrida) |
| **Fila de webhook** com tentativas, limites e degradação | ✅ 🆕 |
| Eventos novos: restrição de conta (`ReachoutTimelock`), erro de conexão, versão desatualizada, erros de pareamento, mudanças de chat (fixar, silenciar, estrela...), bloqueios e mais | 🟡 🆕 (testados de ponta a ponta; vários dependem de situações difíceis de provocar) |
| O aviso **não leva** o `instanceToken` | ✅ 🆕 |

## 9. Chamadas de voz e vídeo

🧪 🆕 Atender, ligar, desligar, controlar vídeo e levar áudio e vídeo a outro sistema, além de um **telefone dentro do painel**. Veja [Chamadas de voz e vídeo](./07-chamadas.md).

## 10. Painel web

✅ 🆕 Painel refeito do zero, com tema claro e escuro: visão geral, gestão de instâncias, conexão por QR/código/passkey, configuração de webhook e eventos, comportamento, **teste de envio de 12 tipos de mensagem**, telefone no navegador e **explorador da API**. Veja [O painel](./05-painel-manager.md).

## 11. Operação: saúde, diagnóstico e métricas

| Funcionalidade | Para que serve | Situação |
|---|---|---|
| `GET /health` | **Pronto para uso?** Testa os bancos de dados; devolve 503 se algum estiver fora. Para o orquestrador/monitor. | ✅ 🆕 |
| `GET /server/ok` | O processo está de pé? | ✅ |
| `GET /instance/{id}/runtime` e `/instance/runtimes` | O que o servidor **realmente** está rodando por instância, comparado com o banco, com avisos codificados (cliente órfão, pareada no banco e nova no runtime...). | ✅ 🆕 |
| `GET /metrics` | Métricas no formato Prometheus (tráfego, eventos, instâncias conectadas, filas, chamadas...). Exige a chave global. | 🆕 |
| `ENABLE_PPROF` | Diagnóstico profundo (para desenvolvedores); desligado por padrão. | ✅ 🆕 |
| Logs por instância | Arquivo por instância, com rotação; o arquivo é liberado quando a instância é apagada. | ✅ 🆕 |
| **Códigos de erro estáveis** | Falhas respondem `{"error": "...", "code": "..."}` com o status certo (503 sem conexão, 409, 429 + `Retry-After`, 404...), em vez de 500 para tudo. | ✅ 🆕 |
| Swagger | `/swagger/index.html`, regenerado com **todas** as rotas (116). | ✅ 🆕 |

## 12. Armazenamento e banco de dados

| Funcionalidade | Situação |
|---|---|
| **PostgreSQL** (dois bancos, `whatygo_auth` e `whatygo_users`; o servidor os cria sozinho) | ✅ |
| **Pool de conexões** limitado, e correção do vazamento que causava `too many clients already` | ✅ 🆕 |
| **Guardar mensagens** (`DATABASE_SAVE_MESSAGES`) | ⬜ |
| **MinIO / S3** para mídias: bucket **privado**, links temporários, apaga ao remover a instância | 🟡 🆕 |
| **SQLite** | Só desenvolvimento; enquetes **não** funcionam sem PostgreSQL. |

## 13. Segurança

Resumo (detalhes em [Configuração e segurança](./08-configuracao-e-seguranca.md)):

- Recusa iniciar com chave de exemplo; compara chaves de forma segura;
- Links de mídia só para endereços públicos; limite de tamanho das requisições; CORS configurável;
- Contêiner roda **sem privilégios de administrador**, com verificação de saúde;
- Painel guarda a sessão só enquanto a aba está aberta e usa política de segurança de conteúdo;
- Senha do proxy nunca devolvida; token fora dos eventos.

---

Quer saber **o que mudou em relação ao original**? Veja [10. O que mudou no fork](./10-o-que-mudou-no-fork.md).
