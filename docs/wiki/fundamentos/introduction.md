# Introdução ao WhatyGo

## Visão Geral

WhatyGo é um gateway de API WhatsApp construído em Go que fornece uma interface RESTful para integração com o protocolo WhatsApp Web. Funciona como camada de abstração entre aplicações e o WhatsApp, permitindo automação de mensagens, gerenciamento de múltiplas instâncias e integração através de APIs padronizadas.

O projeto utiliza a biblioteca [whatsmeow](https://github.com/tulir/whatsmeow), uma implementação Go do protocolo WhatsApp Web Multi-Device, e adiciona sobre ela recursos empresariais como APIs REST, sistema de eventos, persistência de dados e gerenciamento multi-instância.

## O que o WhatyGo oferece

### Comunicação via API REST

Envie e receba mensagens do WhatsApp através de requisições HTTP padrão:
- Mensagens de texto com formatação
- Mídia (imagens, vídeos, áudios, documentos)
- Localização, contatos e stickers
- Mensagens interativas (botões, listas, enquetes)
- Grupos e comunidades

### Gerenciamento Multi-Instância

- Conecte múltiplas contas WhatsApp simultaneamente
- Cada instância opera de forma independente
- Gerenciamento de ciclo de vida completo
- Configuração de proxy por instância
- Logs isolados por instância

### Sistema de Eventos

Receba notificações em tempo real através de:
- **Webhooks** - Callbacks HTTP para serviços externos
- **RabbitMQ** - Filas de mensagens para processamento assíncrono
- **NATS** - Mensageria leve para eventos distribuídos
- **WebSocket** - Conexões em tempo real

### Armazenamento Empresarial

- **Dual Database** - Separação entre dados de autenticação e usuários
- **PostgreSQL** - Banco principal para produção
- **SQLite** - Alternativa para desenvolvimento
- **MinIO/S3** - Armazenamento de mídia escalável
- Pool de conexões otimizado

## Arquitetura

O WhatyGo segue arquitetura em camadas:

```
┌─────────────────────────────────────────┐
│         Camada Handler (HTTP)           │
│    Validação e Roteamento de Requests  │
├─────────────────────────────────────────┤
│         Camada Service                  │
│       Lógica de Negócio e Regras       │
├─────────────────────────────────────────┤
│       Camada Repository                 │
│    Persistência e Acesso a Dados       │
└─────────────────────────────────────────┘
```

### Componentes Principais

- **Instance Manager** - Gerencia ciclo de vida das sessões WhatsApp
- **Whatsmeow Service** - Wrapper sobre a biblioteca whatsmeow
- **Message Service** - Processamento e entrega de mensagens
- **Event System** - Propagação multi-canal de eventos
- **Storage Service** - Gerenciamento de mídia
- **User Service** - Perfis, contatos e configurações
- **Group Service** - Administração de grupos
- **Community Service** - Gerenciamento de comunidades

### Biblioteca Whatsmeow

A [whatsmeow](https://github.com/tulir/whatsmeow) implementa o protocolo WhatsApp Web Multi-Device:
- Comunicação direta com servidores WhatsApp
- Criptografia end-to-end
- Suporte a multi-dispositivos
- Sincronização de mensagens
- Gerenciamento de sessões

O WhatyGo utiliza a whatsmeow como base e adiciona camada empresarial com APIs REST, gerenciamento de múltiplas instâncias, sistema de eventos e persistência.

## Casos de Uso

**Automação de Atendimento**
- Chatbots com respostas automáticas
- Integração com sistemas de tickets
- Roteamento inteligente de conversas

**Notificações Transacionais**
- Confirmações de pedidos
- Alertas de pagamento
- Atualizações de status
- Lembretes automáticos

**Marketing e Campanhas**
- Envio de mensagens em massa
- Segmentação de públicos
- Tracking de engajamento

**Integração com CRM**
- Canal WhatsApp no CRM
- Histórico unificado
- Automação de fluxos

**Comunicação Corporativa**
- Notificações internas
- Alertas de sistemas
- Integração com ERPs

## Stack Tecnológico

- **Linguagem**: Go 1.24+
- **Framework Web**: Gin
- **Protocolo WhatsApp**: whatsmeow
- **ORM**: GORM
- **Banco de Dados**: PostgreSQL / SQLite
- **Filas**: RabbitMQ (AMQP), NATS
- **Storage**: MinIO (S3-compatible)
- **Documentação**: Swagger/OpenAPI
- **Containers**: Docker multi-stage builds

## Endpoints Disponíveis

### Instâncias
- Criar, conectar e gerenciar instâncias
- QR Code para autenticação
- Status e informações de conexão
- Configuração de proxy

### Mensagens
- Envio de texto, mídia, localização
- Botões, listas e enquetes
- Reações e respostas
- Histórico de conversas

### Usuários
- Gerenciamento de perfil
- Contatos e privacidade
- Bloqueios e configurações

### Grupos
- Criação e administração
- Gerenciamento de participantes
- Configurações e permissões

### Chats
- Pin, mute, archive
- Marcação de leitura
- Busca e filtros

### Outros
- Labels (etiquetas)
- Chamadas
- Comunidades
- Newsletters/Canais

## Segurança e Privacidade

- Criptografia end-to-end mantida (protocolo WhatsApp)
- Autenticação via API Key
- Suporte a HTTPS
- Logs de auditoria
- Secrets management
- Isolamento de instâncias

## Telemetria

O sistema coleta métricas anônimas para melhorias:
- Rotas mais utilizadas
- Versão da API em uso
- Estatísticas de performance

**Não são coletados**: mensagens, números de telefone, nomes ou dados pessoais.

## Primeiros Passos

### Requisitos

- Go 1.24+ (instalação local) ou Docker 20.10+
- PostgreSQL 12+ ou SQLite
- Chave de API para autenticação

### Instalação Rápida

```bash
# Clonar repositório
git clone https://github.com/lucasgiovannibr/whatygo.git
cd whatygo

# Configurar ambiente
cp .env.example .env

# Executar com Docker
docker-compose up -d

# Ou executar localmente
make dev
```

O servidor estará disponível em `http://localhost:4000` com documentação Swagger em `/swagger/index.html`.

## Diferencial

| Aspecto | WhatyGo | WhatsApp Business API |
|---------|--------------|----------------------|
| Licença | Apache 2.0 | Proprietária |
| Custo | Gratuito | Pago por mensagem |
| Aprovação | Não requer | Requer aprovação Meta |
| Setup | Minutos | Semanas/meses |
| Multi-instância | Ilimitado | Limitado por contrato |
| Flexibilidade | Total | Limitada por políticas |
| Recursos | Completo | Subconjunto aprovado |

## Documentação

- **[Instalação](./installation.md)** - Guia completo de instalação
- **[Configuração](./configuration.md)** - Variáveis de ambiente
- **[Início Rápido](./quickstart.md)** - Primeiro uso prático
- **[API Overview](../guias-api/api-overview.md)** - Estrutura da API
- **[Arquitetura](../conceitos-core/architecture.md)** - Design do sistema

## Suporte

- **GitHub Issues**: [github.com/lucasgiovannibr/whatygo](https://github.com/lucasgiovannibr/whatygo/issues)
- **Guia para iniciantes**: [docs/guia](../../guia/README.md)

## Licença

Apache License 2.0 com as condições adicionais do projeto original (Evolution Go, da Evolution Foundation), em [LICENSE](../../../LICENSE):

1. Os avisos de copyright do projeto original não podem ser removidos das interfaces
2. O uso do Evolution Go (base do WhatyGo) deve ser notificado/creditado no sistema que o utiliza

Como o WhatyGo cumpre isso: [Avisos legais e créditos](../../guia/11-avisos-legais-e-creditos.md). Texto da Apache 2.0: [apache.org/licenses/LICENSE-2.0](http://www.apache.org/licenses/LICENSE-2.0)

---

**Versão**: 1.0.0
**Mantido por**: mantenedores do WhatyGo (baseado no Evolution Go)
