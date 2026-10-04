# Arquitetura

Como o WhatyGo está organizado por dentro - explicado de forma simples.

## 📋 Índice

- [Visão Geral](#visão-geral)
- [Arquitetura em 3 Camadas](#arquitetura-em-3-camadas)
- [Componentes Principais](#componentes-principais)
- [Como Funciona uma Requisição](#como-funciona-uma-requisição)
- [Organização de Pastas](#organização-de-pastas)
- [Tecnologias Usadas](#tecnologias-usadas)

---

## Visão Geral

O WhatyGo é organizado em **3 camadas** separadas. Pense nisso como uma lanchonete:

1. **Atendente** (Handler) - Recebe seu pedido
2. **Cozinha** (Service) - Prepara o pedido
3. **Estoque** (Repository) - Busca os ingredientes

Cada parte tem uma função específica e não faz o trabalho das outras.

### Por que Separar?

**Vantagens**:
- **Organização**: Cada coisa no seu lugar
- **Manutenção**: Fácil de encontrar e corrigir problemas
- **Testes**: Pode testar cada parte separadamente
- **Escalabilidade**: Fácil de crescer e adicionar funcionalidades

---

## Arquitetura em 3 Camadas

### Camada 1: Handler (Atendente)

**O que faz**: Recebe requisições HTTP e responde ao cliente.

**Responsabilidades**:
- Receber dados do cliente (JSON, formulários)
- Validar se os dados estão corretos
- Chamar o Service para fazer o trabalho
- Devolver a resposta (sucesso ou erro)

**Analogia**: Como o atendente de um restaurante
- Recebe seu pedido
- Anota tudo certinho
- Passa para a cozinha
- Traz sua comida quando fica pronta

**Exemplo do mundo real**:
```
Cliente: "Quero enviar uma mensagem para o número 11999999999"
Handler: "Ok, deixa eu passar isso para o Service..."
Service: "Mensagem enviada!"
Handler: "Pronto, sua mensagem foi enviada!"
```

### Camada 2: Service (Cozinha)

**O que faz**: Implementa a lógica de negócio - onde acontece a "mágica".

**Responsabilidades**:
- Aplicar regras de negócio
- Coordenar entre diferentes partes do sistema
- Validar dados complexos
- Gerenciar transações
- Decidir o que fazer

**Analogia**: Como a cozinha do restaurante
- Recebe o pedido do atendente
- Prepara a comida seguindo a receita
- Coordena entre fogão, forno, geladeira
- Entrega o prato pronto

**Exemplo do mundo real**:
```
Service recebe: "Enviar mensagem para 11999999999"

Service faz:
1. Busca a instância do WhatsApp
2. Verifica se está conectada
3. Formata o número corretamente
4. Monta a mensagem
5. Envia via WhatsApp
6. Salva no banco de dados
7. Avisa o webhook (se tiver)
8. Retorna "Sucesso!"
```

### Camada 3: Repository (Estoque)

**O que faz**: Gerencia os dados do banco de dados.

**Responsabilidades**:
- Salvar dados no banco
- Buscar dados do banco
- Atualizar dados existentes
- Deletar dados

**Analogia**: Como o estoque do restaurante
- Guarda os ingredientes
- Entrega quando a cozinha pede
- Recebe novos ingredientes
- Organiza tudo

**Exemplo do mundo real**:
```
Service: "Preciso das informações da instância 'vendas'"
Repository: "Deixa eu buscar no banco... Aqui está!"

Service: "Salva essa mensagem no banco"
Repository: "Ok, salvei!"
```

---

## Componentes Principais

### 1. Whatsmeow Service

**O que é**: O componente que faz a conexão com o WhatsApp.

**Analogia**: É o "telefone" do sistema. Cada instância tem seu próprio "telefone" para falar com o WhatsApp.

**Funções**:
- Conectar ao WhatsApp
- Manter a conexão ativa
- Enviar mensagens
- Receber mensagens
- Gerenciar sessões

### 2. Event Producer

**O que é**: Componente que avisa outros sistemas quando algo acontece.

**Analogia**: Como um carteiro que entrega notificações.

**Tipos de notificação**:
- **Webhook**: Envia para uma URL sua
- **RabbitMQ**: Envia para fila de mensagens
- **WebSocket**: Envia em tempo real
- **NATS**: Envia para sistema de eventos

**Exemplos de eventos**:
- "Nova mensagem recebida!"
- "QR Code gerado!"
- "Cliente desconectou!"

### 3. Storage Service

**O que é**: Componente que guarda arquivos (fotos, vídeos, áudios).

**Opções**:
- **MinIO/S3**: Para produção (servidor na nuvem)
- **Local**: Para desenvolvimento (pasta no computador)

**Analogia**: Como um HD externo onde ficam salvos os arquivos.

### 4. Config Service

**O que é**: Componente que carrega as configurações do sistema.

**O que configura**:
- Porta do servidor
- Conexão com banco de dados
- API Keys
- URLs de webhooks
- Configurações do WhatsApp

**Analogia**: Como as "regras da casa" - tudo que precisa ser configurado fica aqui.

---

## Como Funciona uma Requisição

### Exemplo: Enviar uma Mensagem de Texto

```
1. VOCÊ
   │
   │ POST /send/text
   │ {"number": "11999999999", "text": "Olá!"}
   │
   ▼
2. HANDLER (Atendente)
   │
   │ "Recebeu um pedido, vou validar..."
   │ ✓ JSON está correto
   │ ✓ Campos obrigatórios presentes
   │
   ▼
3. SERVICE (Cozinha)
   │
   │ "Vou preparar o envio da mensagem..."
   │ 1. Buscar cliente WhatsApp
   │ 2. Verificar se está conectado
   │ 3. Formatar número (11999999999 → 5511999999999@s.whatsapp.net)
   │ 4. Montar mensagem
   │
   ▼
4. WHATSAPP
   │
   │ "Enviando mensagem..."
   │ ✓ Mensagem enviada
   │ Retorna ID: msg_123456
   │
   ▼
5. SERVICE (continuação)
   │
   │ "Mensagem enviada! Agora vou registrar..."
   │ → Salvar no banco via REPOSITORY
   │ → Avisar webhook via EVENT PRODUCER
   │
   ▼
6. HANDLER (continuação)
   │
   │ "Tudo certo! Vou responder o cliente..."
   │
   ▼
7. VOCÊ
   │
   │ Recebe resposta:
   │ {"status": "success", "messageId": "msg_123456"}
```

### Tempo Total

Tudo isso acontece em menos de 1 segundo! ⚡

---

## Organização de Pastas

### Estrutura Simplificada

```
whatygo/
│
├── cmd/
│   └── whatygo/
│       └── main.go          ← Arquivo principal (inicia tudo)
│
├── pkg/                     ← Código principal
│   │
│   ├── instance/            ← Gerenciar instâncias
│   │   ├── handler/         ← Recebe requisições HTTP
│   │   ├── service/         ← Lógica de negócio
│   │   ├── repository/      ← Acesso ao banco
│   │   └── model/           ← Estrutura de dados
│   │
│   ├── message/             ← Gerenciar mensagens
│   │   ├── handler/
│   │   ├── service/
│   │   ├── repository/
│   │   └── model/
│   │
│   ├── group/               ← Gerenciar grupos
│   ├── user/                ← Gerenciar usuários
│   ├── chat/                ← Gerenciar chats
│   │
│   ├── whatsmeow/           ← Conexão WhatsApp
│   ├── events/              ← Sistema de eventos
│   ├── storage/             ← Armazenar arquivos
│   └── config/              ← Configurações
│
├── docs/                    ← Documentação
├── logs/                    ← Arquivos de log
└── .env                     ← Configurações (senhas, chaves)
```

### Padrão de Organização

Cada funcionalidade tem a mesma estrutura:

```
funcionalidade/
├── handler/      ← Recebe requisições
├── service/      ← Processa lógica
├── repository/   ← Acessa banco
└── model/        ← Define estruturas
```

**Exemplo**: Para enviar mensagens, você tem:
- `message/handler/` - Recebe a requisição
- `message/service/` - Prepara e envia
- `message/repository/` - Salva no banco
- `message/model/` - Define como é uma mensagem

---

## Tecnologias Usadas

### Framework Web

**Gin** - Framework HTTP para Go
- Rápido e leve
- Fácil de usar
- Gerencia rotas e middlewares

**O que faz**: Recebe requisições HTTP e encaminha para os handlers.

### Cliente WhatsApp

**Whatsmeow** - Biblioteca oficial
- Implementa protocolo do WhatsApp Web
- Multi-dispositivo
- Criptografia ponta-a-ponta

**O que faz**: Conversa com os servidores do WhatsApp.

### Banco de Dados

**GORM** - ORM (Mapeador Objeto-Relacional)
- Facilita trabalhar com banco de dados
- Suporta PostgreSQL e SQLite
- Migrations automáticas

**O que faz**: Converte dados entre o código e o banco de dados.

### Filas de Mensagem

**RabbitMQ** - Sistema de filas
**NATS** - Sistema de eventos
- Entregam notificações de forma confiável
- Processamento assíncrono

**O que fazem**: Avisam outros sistemas quando algo acontece.

### Armazenamento de Arquivos

**MinIO** - Storage compatível com S3
- Armazena fotos, vídeos, áudios
- Escalável

**O que faz**: Guarda arquivos de mídia enviados/recebidos.

### Utilitários

**QRCode** - Gera QR Codes
**UUID** - Gera IDs únicos
**WebSocket** - Comunicação em tempo real
**Zap Logger** - Logs estruturados

---

## Ciclo de Vida de uma Instância

### Estados

```
1. CRIADA
   ↓
   (usuário cria via API)
   ↓
2. DESCONECTADA
   ↓
   (usuário conecta)
   ↓
3. CONECTANDO
   ↓
   (gera QR Code)
   ↓
4. AGUARDANDO SCAN
   ↓
   (usuário escaneia QR no celular)
   ↓
5. ABERTA/CONECTADA
   ↓
   (pode enviar/receber mensagens)
   ↓
   (se desconectar)
   ↓
6. DESCONECTADA
   ↓
   (pode reconectar sem QR)
   ↓
5. ABERTA/CONECTADA
```

### Reconexão, desligamento e réplicas

- **Queda de conexão**: o servidor reconecta sozinho (primeira tentativa imediata; depois 5 s, 10 s, 20 s... até 5 min, com jitter; uma conexão estável de 1 minuto zera a contagem). Um pedido de reconexão pela API não é espaçado.
- **Disconnect pela API**: a instância fica desligada até um `connect` explícito; requisições que precisariam do cliente respondem `409 instance_disconnected_by_user` em vez de reconectar.
- **Partida em massa** (`CONNECT_ON_STARTUP`): uma instância a cada ~300 ms.
- **Parada do servidor**: os clientes são desconectados sem alterar o estado salvo, então as instâncias voltam no próximo start; as filas de webhook são esvaziadas até o prazo.
- **Várias réplicas**: uma réplica por instância (lock consultivo do Postgres numa sessão dedicada). Se a réplica morre ou perde a conexão com o banco, o lock se solta; ao reconectar, ela retoma os seus e só abre mão de uma instância que outra réplica realmente assumiu.

### Ações Possíveis

| Estado | O que Pode Fazer |
|--------|------------------|
| **Criada** | Conectar, Deletar |
| **Desconectada** | Conectar, Reconectar, Deletar |
| **Conectando** | Aguardar, Cancelar |
| **Aguardando QR** | Escanear QR, Timeout |
| **Aberta** | Enviar mensagens, Desconectar, Logout |

---

## Segurança

### 1. Autenticação

Todas as requisições precisam de API Key:
- Administrador: API Key Global
- Instância: Token específico

### 2. Isolamento

Cada instância é completamente isolada:
- Não pode acessar dados de outras instâncias
- Tem seu próprio cliente WhatsApp
- Sessão separada no banco

### 3. Validação

Todos os dados são validados antes de processar:
- Formato de número de telefone
- Tamanho de mensagens
- Tipos de arquivo permitidos

### 4. Criptografia

WhatsApp usa criptografia ponta-a-ponta:
- Mensagens criptografadas automaticamente
- Chaves guardadas de forma segura
- Ninguém pode interceptar

---

## Escalabilidade

### Horizontal (Mais Servidores)

Pode ter múltiplas cópias do WhatyGo:

```
           ┌─────────────────┐
           │ Load Balancer   │
           └────────┬─────────┘
                    │
        ┌───────────┼───────────┐
        │           │           │
   ┌────▼───┐  ┌───▼────┐  ┌───▼────┐
   │Server 1│  │Server 2│  │Server 3│
   └────┬───┘  └───┬────┘  └───┬────┘
        │          │           │
        └──────────┼───────────┘
                   │
            ┌──────▼──────┐
            │  PostgreSQL │
            └─────────────┘
```

**Vantagens**:
- Suporta mais usuários
- Se um servidor cair, outros continuam
- Distribuição de carga

### Vertical (Servidor Mais Potente)

Melhorar o servidor existente:
- Mais CPU
- Mais RAM
- Mais espaço em disco

**Quando usar**:
- Até ~100 instâncias: Vertical
- Mais de 100: Horizontal

---

## Resumo Visual

### Fluxo Completo

```
    REQUISIÇÃO
        ↓
    ┌────────┐
    │Handler │ ← Valida dados
    └───┬────┘
        ↓
    ┌────────┐
    │Service │ ← Aplica lógica
    └───┬────┘
        ↓
    ┌────────────┐
    │Repository  │ ← Salva/busca dados
    └───┬────────┘
        ↓
    ┌────────┐
    │Database│
    └────────┘
```

### Componentes Interconectados

```
┌──────────────────────────────────┐
│      WhatyGo Server         │
├──────────────────────────────────┤
│                                  │
│  ┌────────┐      ┌───────────┐  │
│  │Handler │─────>│ Service   │  │
│  └────────┘      └─────┬─────┘  │
│                        │         │
│         ┌──────────────┼────────┐│
│         │              │        ││
│    ┌────▼───┐    ┌────▼────┐   ││
│    │Whatsapp│    │Repository   ││
│    │Service │    └────┬────┘   ││
│    └────┬───┘         │        ││
└─────────┼─────────────┼────────┘│
          │             │          
      ┌───▼────┐    ┌───▼───┐     
      │WhatsApp│    │  DB   │     
      │Servers │    │       │     
      └────────┘    └───────┘     
```

---

## Resumo Rápido

| Conceito | Explicação Simples |
|----------|-------------------|
| **Handler** | Atendente - recebe pedidos |
| **Service** | Cozinha - prepara tudo |
| **Repository** | Estoque - guarda dados |
| **Whatsmeow** | Telefone - fala com WhatsApp |
| **Events** | Carteiro - avisa outros sistemas |
| **Storage** | HD - guarda arquivos |

**Lembre-se**:
- 📱 Cada camada tem uma função específica
- 🔄 Requisição passa por todas as camadas
- 🏗️ Organização facilita manutenção
- 🚀 Pode escalar horizontal ou verticalmente
- 🔒 Segurança em todas as camadas

---

**Documentação WhatyGo v1.0**
