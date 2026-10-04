# 1. Visão geral

## O que é o WhatyGo

O WhatyGo é um **servidor que conecta o WhatsApp a outros sistemas**. Você liga um número de WhatsApp a ele (do mesmo jeito que liga o WhatsApp Web, lendo um QR Code) e, a partir daí:

- **seu sistema manda mensagens** pelo WhatsApp chamando o servidor (por exemplo: um site que avisa o cliente que o pedido saiu);
- **seu sistema recebe as mensagens** que chegam nesse número (o servidor avisa o seu sistema na hora);
- você também pode **usar o painel web** que vem junto, sem programar nada, para conectar números, testar envios e até atender ligações.

É o mesmo tipo de ferramenta usado para chatbots, atendimento, notificações automáticas e integrações com CRMs.

> **Origem.** O WhatyGo é um fork do Evolution Go: parte do código e da ideia vêm de lá, e a conexão com o WhatsApp é feita pela biblioteca [whatsmeow](https://github.com/tulir/whatsmeow). Este fork nasceu para **corrigir, melhorar e ajustar** o projeto original, e hoje tem muitas diferenças (veja [O que mudou no fork](./10-o-que-mudou-no-fork.md)).

## Um desenho simples

```
   Seu sistema / site / chatbot
        │  ▲
  envia │  │ avisa quando chega mensagem
        ▼  │ (webhook, WebSocket, fila...)
   ┌──────────────────────┐        ┌────────────────────┐
   │       WhatyGo        │◄──────►│ Banco de dados     │
   │  (servidor + painel) │        │ (PostgreSQL)       │
   └──────────┬───────────┘        └────────────────────┘
              │
              ▼
          WhatsApp  ◄──── seu celular (continua funcionando normalmente)
```

## Palavras que aparecem muito

| Termo | O que quer dizer |
|---|---|
| **Instância** | Um número de WhatsApp conectado ao servidor. Você pode ter várias instâncias no mesmo servidor, cada uma com seu número. |
| **QR Code / pareamento** | A forma de ligar o número: no celular, WhatsApp → *Aparelhos conectados* → *Conectar um aparelho*, e ler o QR Code. Também existe o **código por telefone** (um código de 8 caracteres, para quem não consegue ler o QR). |
| **API** | A "porta de entrada" do servidor: endereços (`/send/text`, `/instance/create`...) que o seu sistema chama para mandar ordens. |
| **API Key / chave** | Uma senha enviada a cada chamada. Há duas: a **chave global** (administrador: cria e apaga instâncias) e o **token da instância** (só mexe naquele número). |
| **Webhook** | Um endereço do **seu** sistema para onde o servidor envia um aviso a cada novidade (mensagem recebida, conexão caiu...). |
| **Evento** | Cada tipo de novidade: mensagem recebida, mensagem lida, alguém ficou online, ligação recebida etc. |
| **Painel / Manager** | O site que o próprio servidor entrega em `/manager`, para operar tudo pelo navegador. |
| **Docker** | Programa que roda o servidor "dentro de uma caixa" pronta, sem você instalar nada manualmente. É a forma recomendada de instalar. |
| **Swagger** | Uma página (em `/swagger/index.html`) que lista todos os endereços da API e deixa testá-los. |

## O que você precisa ter

- Um computador ou servidor (Windows, Mac ou Linux) com **Docker** instalado. Para uso real, um servidor (VPS) com pelo menos **1 GB de memória e 2 GB de disco**.
- Um **número de WhatsApp** (de preferência um chip dedicado ao projeto, não o seu pessoal).
- Um navegador moderno (Chrome, Edge ou Safari recentes).

## O que ele **não** é

- **Não é o WhatsApp oficial.** O WhatyGo usa o mesmo caminho do WhatsApp Web. Isso funciona, mas **o WhatsApp não aprova automações não oficiais**: contas que mandam mensagens em massa, repetidas ou para quem não pediu podem ser **banidas**. Use com responsabilidade e comece devagar (veja [Configuração e segurança](./08-configuracao-e-seguranca.md#cuidados-para-não-ser-banido)).
- **Não é um aplicativo de atendimento pronto.** Ele é a "ponte"; quem dá inteligência (chatbot, CRM, filas de atendimento) é o sistema que você liga nele.
- **Não grava ligações.** De propósito, por causa de privacidade e questões legais.

## Para quem serve

| Perfil | Uso típico |
|---|---|
| Pequena empresa / autônomo | Conectar o WhatsApp a um CRM ou chatbot; avisos automáticos de pedido, agendamento, cobrança. |
| Agência / integrador | Várias instâncias (uma por cliente) num só servidor, com webhook para cada sistema. |
| Desenvolvedor | API REST com eventos em tempo real (webhook, WebSocket, RabbitMQ, NATS) e Swagger. |
| Curioso / estudante | Painel para testar mensagens, botões, enquetes e ligações sem escrever código. |

Próximo passo: [2. Instalação](./02-instalacao.md).
