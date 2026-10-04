# Banco de Dados

Sistema de armazenamento de dados do WhatyGo usando bancos separados para diferentes finalidades.

## 📋 Índice

- [Visão Geral](#visão-geral)
- [Por que Dois Bancos](#por-que-dois-bancos)
- [Banco Auth](#banco-auth)
- [Banco Users](#banco-users)
- [Tabelas Principais](#tabelas-principais)
- [Como os Dados se Relacionam](#como-os-dados-se-relacionam)
- [Boas Práticas](#boas-práticas)

---

## Visão Geral

O WhatyGo usa **dois bancos de dados separados**. Pense nisso como ter dois armários diferentes:

1. **Banco Auth** (`whatygo_auth`): Guarda os dados técnicos do WhatsApp
2. **Banco Users** (`whatygo_users`): Guarda os dados da sua aplicação

### Tipos de Banco Suportados

- **PostgreSQL**: Recomendado para produção (servidores reais)
- **SQLite**: Usado para desenvolvimento e testes (arquivo local)

---

## Por que Dois Bancos

### Analogia do Armário

Imagine que você tem uma loja:
- **Armário 1** (Auth): Guarda as chaves, segredos e equipamentos técnicos
- **Armário 2** (Users): Guarda produtos, clientes e vendas

É mais organizado e seguro manter separado!

### Vantagens

1. **Organização**: Dados do WhatsApp separados dos dados da sua API
2. **Segurança**: Backup independente para cada tipo de dado
3. **Performance**: Cada banco pode ser otimizado para seu uso
4. **Escalabilidade**: Pode colocar cada banco em um servidor diferente

---

## Banco Auth

### O que Guarda

Todos os dados técnicos do WhatsApp (biblioteca whatsmeow):

- **Chaves de criptografia** - Para proteger suas mensagens
- **Informações do dispositivo** - Identificação do seu WhatsApp
- **Sessões ativas** - Conexões abertas
- **Contatos sincronizados** - Lista de contatos

### Tabelas Importantes

O WhatsApp cria automaticamente várias tabelas:

1. **whatsmeow_device** - Seu "telefone virtual"
2. **whatsmeow_identity_keys** - Chaves de segurança
3. **whatsmeow_sessions** - Sessões de conversa
4. **whatsmeow_contacts** - Lista de contatos
5. ... e outras 6-7 tabelas técnicas

⚠️ **IMPORTANTE**: Não mexa nessas tabelas manualmente! O WhatsApp gerencia tudo automaticamente.

### Por que é Crítico

Este banco contém as **chaves de criptografia**. Sem backup dele:
- ✅ Com backup: Reconecta automaticamente após reiniciar
- ❌ Sem backup: Precisa escanear QR Code novamente

---

## Banco Users

### O que Guarda

Todos os dados da **sua aplicação**:

- **Instâncias** - Cada WhatsApp conectado
- **Mensagens** - Histórico de mensagens enviadas
- **Labels** - Etiquetas/marcações
- **Configurações** - Webhooks, eventos, etc.

### Tabelas Principais

#### 1. Tabela `instances`

**O que é**: A tabela mais importante! Cada linha é um WhatsApp conectado.

**Informações guardadas**:
- **id**: Identificador único (tipo CPF)
- **name**: Nome amigável (ex: "vendas", "suporte")
- **token**: Chave de acesso da instância
- **webhook**: URL para receber eventos
- **jid**: Número do WhatsApp (ex: 5511999999999@s.whatsapp.net)
- **connected**: Está conectado? (true/false)
- **qrcode**: QR Code para conectar
- **events**: Quais eventos quer receber

**Configurações Avançadas**:
- **always_online**: Aparecer sempre online
- **reject_call**: Rejeitar chamadas automaticamente
- **read_messages**: Marcar mensagens como lidas
- **ignore_groups**: Ignorar mensagens de grupo
- **ignore_status**: Ignorar status/stories

#### 2. Tabela `messages`

**O que é**: Histórico de mensagens (opcional, pode ser desabilitado).

**Informações guardadas**:
- **id**: Identificador único
- **message_id**: ID da mensagem no WhatsApp
- **timestamp**: Quando foi enviada
- **status**: Status (enviada, entregue, lida)
- **source**: De qual instância veio

💡 **Dica**: Por padrão, não salvamos o conteúdo completo das mensagens por questão de espaço.

#### 3. Tabela `labels`

**O que é**: Etiquetas/marcações do WhatsApp.

**Informações guardadas**:
- **id**: Identificador único
- **instance_id**: De qual instância
- **label_name**: Nome da etiqueta (ex: "Cliente VIP")
- **label_color**: Cor da etiqueta (0-19)

---

## Tabelas Principais

### Estrutura Visual

```
┌──────────────┐
│  instances   │  (Tabela Principal)
│              │
│ - id         │◄─────────┐
│ - name       │          │
│ - token      │          │  Relacionamento
│ - connected  │          │
└──────────────┘          │
                       │
           ┌──────────────┴──────────────┐
           │                             │
    ┌──────▼──────┐             ┌───────▼──────┐
    │  messages   │             │   labels     │
    │             │             │              │
    │ - id        │             │ - id         │
    │ - source ───┼─────┐       │ - instance_id│───┐
    │   (FK)      │     │       │   (FK)       │   │
    └─────────────┘     │       └──────────────┘   │
                        │                           │
                        └────── Aponta para ────────┘
                              instances.id
```

**FK = Foreign Key (Chave Estrangeira)**
- Significa que aponta para outra tabela
- Como um "link" entre tabelas

### Quando Deleta uma Instância

Se você deletar uma instância, automaticamente deleta:
- ✅ Todas as mensagens dessa instância
- ✅ Todas as labels dessa instância

Isso se chama **deleção em cascata** - como um efeito dominó!

---

## Como os Dados se Relacionam

### Exemplo Prático

Imagine que você tem:

**1 Instância**:
```
ID: abc-123
Nome: vendas
Token: token-vendas-123
```

**3 Mensagens desta instância**:
```
Mensagem 1: source = abc-123
Mensagem 2: source = abc-123
Mensagem 3: source = abc-123
```

**2 Labels desta instância**:
```
Label 1: instance_id = abc-123
Label 2: instance_id = abc-123
```

Se você deletar a instância `abc-123`:
- ❌ Instância deletada
- ❌ 3 mensagens deletadas automaticamente
- ❌ 2 labels deletadas automaticamente

---

## Configuração

### Variáveis de Ambiente

```env
# Banco Auth (Dados do WhatsApp)
POSTGRES_AUTH_DB=postgresql://user:pass@localhost:5432/whatygo_auth

# Banco Users (Dados da API)
POSTGRES_USERS_DB=postgresql://user:pass@localhost:5432/whatygo_users
```

### Criação Automática de Tabelas

Quando você inicia o WhatyGo pela primeira vez:

1. Sistema verifica se as tabelas existem
2. Se não existir, cria automaticamente
3. Se existir, apenas conecta
4. **Nunca remove ou altera tabelas existentes** (seguro!)

Isso se chama **Auto-Migration** (migração automática).

---

## Boas Práticas

### 1. Faça Backup Regular

**Recomendado**: Backup diário automático

```bash
# Backup do banco Auth (CRÍTICO!)
pg_dump -U postgres whatygo_auth > backup_auth_$(date +%Y%m%d).sql

# Backup do banco Users
pg_dump -U postgres whatygo_users > backup_users_$(date +%Y%m%d).sql
```

💡 O backup do **Auth** é mais crítico pois contém as chaves de criptografia!

### 2. Monitorar Espaço

Tabelas crescem com o tempo:

| Tabela | Crescimento |
|--------|-------------|
| **messages** | Rápido (se salvar mensagens) |
| **instances** | Lento |
| **labels** | Lento |
| **whatsmeow_***| Médio |

**Recomendação**: Limpar mensagens antigas regularmente.

### 3. Connection Pooling

**O que é**: Reutilizar conexões ao banco ao invés de abrir/fechar toda hora.

**Configurações recomendadas**:

**Desenvolvimento** (computador local):
- Máximo 20 conexões simultâneas
- 5 conexões em stand-by

**Produção** (servidor):
- Máximo 100 conexões simultâneas
- 25 conexões em stand-by

### 4. Índices para Performance

**O que são índices**: Como um índice de livro - ajuda a encontrar coisas mais rápido!

**Índices importantes já criados**:
- `instances.name` - Buscar por nome
- `instances.token` - Buscar por token
- `messages.source` - Buscar mensagens de uma instância
- `labels.instance_id` - Buscar labels de uma instância

### 5. Limpeza de Dados

**Recomendações**:

```bash
# Deletar mensagens com mais de 90 dias
DELETE FROM messages WHERE timestamp < NOW() - INTERVAL '90 days';

# Deletar instâncias desconectadas há mais de 30 dias
DELETE FROM instances 
WHERE connected = false 
AND updated_at < NOW() - INTERVAL '30 days';
```

---

## Troubleshooting

### Erro: "too many connections"

**Problema**: Muitas conexões abertas ao banco.

**Soluções**:
1. Verifique quantas conexões o PostgreSQL permite
2. Reduza o número de conexões máximas no WhatyGo
3. Aumente o limite no PostgreSQL

```bash
# Ver limite atual
psql -c "SHOW max_connections;"

# Aumentar para 200 (editar postgresql.conf)
max_connections = 200
```

### Erro: "table does not exist"

**Problema**: Tabela não foi criada.

**Solução**: Reinicie a aplicação. Ela cria as tabelas automaticamente.

### Banco crescendo muito

**Problema**: Banco de dados está ocupando muito espaço.

**Diagnóstico**:
```sql
-- Ver tamanho de cada tabela
SELECT 
  tablename,
  pg_size_pretty(pg_total_relation_size(tablename::text)) as size
FROM pg_tables
WHERE schemaname = 'public'
ORDER BY pg_total_relation_size(tablename::text) DESC;
```

**Solução**: Limpe mensagens antigas ou desabilite o salvamento de mensagens.

### Queries lentas

**Problema**: Operações demorando muito.

**Solução**: Provavelmente falta um índice. Consulte um DBA (administrador de banco de dados).

---

## Resumo Visual

### Separação de Responsabilidades

```
┌────────────────────────┐         ┌────────────────────────┐
│   Banco Auth           │         │   Banco Users          │
│   (whatygo_auth)         │         │   (whatygo_users)        │
├────────────────────────┤         ├────────────────────────┤
│                        │         │                        │
│ WhatsApp               │         │ Sua Aplicação          │
│ Dados Técnicos         │         │ Dados de Negócio       │
│                        │         │                        │
│ • Chaves cripto        │         │ • Instâncias           │
│ • Sessões              │         │ • Mensagens            │
│ • Device info          │         │ • Labels               │
│ • Contatos             │         │ • Webhooks             │
│                        │         │ • Configurações        │
└────────────────────────┘         └────────────────────────┘
         ▲                                   ▲
         │                                   │
         └───────── Gerenciado por ──────────┘
                   WhatyGo
```

### Fluxo de Dados

```
1. Usuário cria instância
   ↓
2. Salvo em: users → instances

3. Usuário conecta WhatsApp
   ↓
4. WhatsApp salva sessão em: auth → whatsmeow_device

5. Mensagem enviada
   ↓
6. Salvo em: users → messages

7. Label criada
   ↓
8. Salvo em: users → labels
```

---

## Resumo Rápido

| Conceito | Explicação |
|----------|------------|
| **2 Bancos** | Auth (WhatsApp) + Users (API) |
| **Auto-Migration** | Cria tabelas automaticamente |
| **Cascata** | Deletar instância → deleta tudo relacionado |
| **Backup** | Crítico para Auth (chaves cripto) |
| **Índices** | Fazem buscas ficarem rápidas |

**Lembre-se**:
- 🔐 Banco Auth = Dados do WhatsApp (crítico!)
- 📊 Banco Users = Dados da sua aplicação
- 💾 Faça backup regular
- 🧹 Limpe dados antigos
- 📈 Monitore crescimento

---

**Documentação WhatyGo v1.0**
