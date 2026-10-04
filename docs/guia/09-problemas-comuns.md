# 9. Problemas comuns

Procure o sintoma. Para ver o que está acontecendo, o primeiro passo quase sempre é olhar os logs:

```bash
docker compose logs -f whatygo
```

## Instalação e acesso

### O servidor não sobe e o log diz que a `GLOBAL_API_KEY` é "um valor publicado"
Você usou uma chave de exemplo. Gere uma própria (`openssl rand -hex 32`), coloque no `.env` e rode `docker compose up -d`. (Só para testar localmente existe `ALLOW_INSECURE_API_KEY=true`; não use em servidor real.)

### `docker compose` reclama que falta `POSTGRES_PASSWORD` ou `GLOBAL_API_KEY`
O arquivo `.env` não existe ou está vazio. Copie o `.env.example` para `.env` **na mesma pasta do `docker-compose.yml`** e preencha.

### A porta 4000 já está em uso
Mude `HTTP_PORT=4001` no `.env` e rode `docker compose up -d` de novo; depois acesse `http://localhost:4001/manager`.

### A API responde `503` com `LICENSE_REQUIRED`
Essa resposta vem de uma versão **antiga** (a do projeto original, ou do WhatyGo antes de a ativação de licença ser removida). A versão atual não tem mais essa barreira: atualize a imagem ([Atualizar para uma versão nova](./02-instalacao.md#atualizar-para-uma-versão-nova)) e a API responde normalmente, sem registrar nada ([Instalação](./02-instalacao.md#não-há-ativação-de-licença)).

### Uma integração que chamava `/license/status` recebe `404`
As rotas `/license/status`, `/license/register` e `/license/activate` foram removidas junto com a ativação. Tire essa verificação da integração: use `GET /server/ok` (o servidor está de pé) ou `GET /health` (os bancos respondem).

### O painel abre, mas "não foi possível carregar os dados"
O painel não alcança a API. Confira se o servidor está de pé (`curl http://localhost:4000/health`) e, se o painel e a API estão em endereços diferentes, use **Alterar** na tela de entrada para informar o endereço da API.

### `/health` devolve `503`
Um dos bancos de dados não responde. Veja `docker compose ps` (o `postgres` deve estar *healthy*) e os logs do banco. Se disser `slow`, o banco está sobrecarregado.

## Conexão do WhatsApp

### O QR Code não aparece
Veja os logs; costuma ser o banco fora do ar. Se persistir, apague a instância e crie de novo.

### O QR Code expirou
Vale cerca de 40 s e se renova até `QRCODE_MAX_COUNT` vezes; depois disso a instância **para** (`disconnect_reason: "QR code timeout"`). Clique em conectar de novo (ou abra o QR no painel) e leia mais rápido.

### Conectei e a instância "caiu" sozinha
- Se o celular ficou sem internet por muito tempo ou o aparelho foi removido em *Aparelhos conectados*, a sessão acaba e é preciso ler o QR de novo.
- O servidor tenta reconectar sozinho, esperando mais a cada falha (até 5 min). Veja o motivo em `GET /instance/{id}/runtime` (campo de avisos).
- `405 / client outdated` nos logs: o WhatsApp recusou a versão do cliente. O WhatyGo descarta a versão guardada e pega a atual na próxima tentativa; aguarde a reconexão.

### Depois de reiniciar o servidor os números não voltam
Confira `CONNECT_ON_STARTUP=true` (a instalação simples já liga). Instância que você **desconectou pela API** não volta sozinha de propósito: use **Conectar**.

### A instância diz que está "pareada no banco, mas é um dispositivo novo"
É um aviso do diagnóstico (`/instance/{id}/runtime`): o runtime e o banco discordam. Desconecte, apague a instância e conecte de novo.

## Envio

### "Falha no envio" ou erro `503` ao enviar
A instância não está conectada. Veja o estado na aba **Geral**; se estiver desconectada, conecte.

### Erro `409`
O código do erro (`code`) diz o motivo: instância não pareada, desconectada pela API ou rodando em outro servidor (`INSTANCE_LOCK`).

### Erro `429`
Você enviou rápido demais. Respeite o cabeçalho `Retry-After` e ajuste `SEND_RATE_PER_MIN`.

### Erro `463` ("reachout")
O WhatsApp limitou a conta temporariamente. Pare de enviar até a data informada. Vem de mandar para muitos desconhecidos.

### Mensagem enviada, mas não chegou ou chegou "vazia"
- Confirme o número: DDI + DDD + número, só dígitos (`5511999999999`). No Brasil, confira o 9º dígito.
- Botões, carrosséis e listas podem ser **aceitos pelo servidor e descartados pelo WhatsApp** (`200` não garante). Veja [Mensagens interativas](./04-funcionalidades.md#3-mensagens-interativas).
- Se foi a **mensagem recebida** que não decifrou, o evento `UndecryptableMessage` avisa e `POST /message/rerequest` pede o reenvio ao celular.

### Lista não aparece como lista
O WhatsApp não aceita lista de aparelho vinculado; o servidor a manda como **botões de resposta**. É esperado.

## Receber eventos

### Meu webhook não recebe nada
1. A instância está conectada?
2. O endereço do webhook está certo e acessível **pelo servidor** (de dentro do Docker, `localhost` é o próprio contêiner!)?
3. Os eventos desejados estão marcados?
4. Veja `GET /instance/runtimes`: mostra o estado e as estatísticas da fila de cada destino.

### Estava recebendo e parou, e agora vêm eventos atrasados
O destino ficou fora do ar, e o servidor entrou em modo de tentativa com espera crescente. Quando ele voltar, a fila é esvaziada. Se ela estourou (1000 eventos), os mais antigos foram descartados.

### Não recebo mais o `instanceToken` no webhook
É uma mudança de segurança do WhatyGo. `WEBHOOK_INCLUDE_TOKEN=true` o traz de volta.

## Chamadas

### O motor de chamadas diz `blocked_by_proxy`
A instância usa proxy, e o motor não funciona assim. Veja [Chamadas](./07-chamadas.md#ligar-o-motor).

### Liguei o interruptor e nada mudou
Vale só na **próxima conexão**. Desconecte e conecte de novo.

### O outro lado se ouve de volta (eco)
Use fones de ouvido.

### O painel não deixa usar o microfone
O navegador só libera o microfone em `https` ou `localhost`.

## Manutenção

### Tem pouca memória/disco
A tabela de mensagens (`DATABASE_SAVE_MESSAGES=true`) só cresce. Desligue se não precisa. Os logs têm rotação.

### Como zero tudo e recomeço?
`docker compose down -v` apaga **tudo**, inclusive os números conectados (você terá de ler os QR Codes de novo).

## Ainda com problema?

Abra uma *issue* no repositório do WhatyGo com: o que você fez, o que esperava, o que aconteceu, e as últimas linhas do log. **Antes de colar logs, apague chaves, tokens e números de telefone.**

Se for uma falha de segurança, **não** abra issue pública: veja o [SECURITY.md](../../SECURITY.md).
