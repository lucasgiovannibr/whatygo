# 3. Primeiros passos

Neste capítulo você vai conectar um número de WhatsApp e mandar a primeira mensagem, **tudo pelo painel**, sem programar. Pré-requisito: o servidor instalado ([Instalação](./02-instalacao.md)).

## 1. Entrar no painel

1. Abra `http://localhost:4000/manager` (ou o endereço do seu servidor).
2. Digite a **chave global** (`GLOBAL_API_KEY`) e clique em **Entrar**.

A sessão dura enquanto a aba estiver aberta. Ao fechar a aba, será preciso entrar de novo (é uma proteção, e não um defeito).

## 2. Criar uma instância

Uma instância é um número de WhatsApp. Na página **Instâncias**, clique em criar e preencha:

| Campo | O que é |
|---|---|
| **Nome** | Identifica a instância (por exemplo `loja` ou `atendimento`). **Não pode ser mudado depois.** |
| **Token de acesso** | A "senha" desta instância na API. O painel gera um sozinho; só troque se tiver um motivo. |
| **Proxy** (opcional) | Servidor intermediário para a conexão do número. A maioria das pessoas não usa. Instâncias com proxy **não** podem usar o motor de chamadas. |

## 3. Conectar o número

Na instância recém-criada, clique em **Conectar**. Há duas formas:

### Opção A: QR Code (a mais comum)

1. No celular: WhatsApp → **Configurações** (ou os três pontinhos) → **Aparelhos conectados** → **Conectar um aparelho**.
2. Aponte a câmera para o QR Code que aparece no painel.
3. Aguarde alguns segundos. O painel avisa que conectou.

O QR Code vale cerca de 40 segundos e se renova sozinho algumas vezes (por padrão, 5 tentativas). Se acabar, é só clicar em conectar de novo.

### Opção B: código por telefone

Na janela de conexão, escolha **Código por telefone**, informe o número com DDI e DDD (`5511999999999`) e digite no celular o código de 8 caracteres que aparecer: WhatsApp → **Aparelhos conectados** → **Conectar um aparelho** → **Conectar com número de telefone**.

### Quando o WhatsApp pede "chave de acesso" (passkey)

Algumas contas exigem uma chave de acesso (biometria ou PIN) para ligar um aparelho, e não mostram QR Code. O painel guia: instale a extensão **Passkey Helper** no Chrome ou Edge (está na pasta `passkey-helper/` do projeto), abra o WhatsApp Web pelo botão do painel e confirme com biometria. Para esse botão aparecer, o servidor precisa da variável `PASSKEY_PUBLIC_URL` (um endereço que o seu navegador alcança). 🟡 *Este caminho foi melhorado no WhatyGo, mas não pôde ser testado com uma conta que exija passkey.*

## 4. Mandar a primeira mensagem

1. Abra a instância e vá na aba **Testar envio**.
2. Escolha o tipo (comece por **Texto**).
3. Informe o **número de destino** com DDI e DDD, só dígitos: `5511999999999`.
4. Escreva a mensagem e envie.

Se aparecer **Enviado com sucesso**, confira no celular de destino. Se der **Falha no envio**, veja [Problemas comuns](./09-problemas-comuns.md).

> **Cuidado:** a aba de testes manda mensagens **de verdade**. Use um número seu para testar.

## 5. Receber mensagens

Para o seu sistema saber que chegou uma mensagem, configure um **webhook** (aba **Webhook e eventos**). O passo a passo está em [Receber mensagens e eventos](./06-receber-mensagens-e-eventos.md).

## 6. Fazer a mesma coisa pela API (para quem vai programar)

Tudo o que o painel faz, a API faz. O mínimo:

```bash
# 1. criar a instância (chave global)
curl -X POST http://localhost:4000/instance/create \
  -H "Content-Type: application/json" -H "apikey: SUA_CHAVE_GLOBAL" \
  -d '{"name":"loja","token":"um-token-secreto-qualquer"}'

# 2. iniciar a conexão e pegar o QR Code (token da instância)
curl -X POST http://localhost:4000/instance/connect -H "apikey: um-token-secreto-qualquer"
curl http://localhost:4000/instance/qr -H "apikey: um-token-secreto-qualquer"

# 3. enviar um texto
curl -X POST http://localhost:4000/send/text \
  -H "Content-Type: application/json" -H "apikey: um-token-secreto-qualquer" \
  -d '{"number":"5511999999999","text":"Olá!"}'
```

A lista completa está no Swagger (`/swagger/index.html`) e no [guia técnico](../wiki/fundamentos/quickstart.md).

Próximo passo: ver tudo o que dá para fazer em [4. Funcionalidades](./04-funcionalidades.md).
