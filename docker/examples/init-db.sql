-- Script de inicialização dos bancos de dados WhatyGo

-- Criar database para autenticação
CREATE DATABASE whatygo_auth;

-- Criar database para dados de usuários
CREATE DATABASE whatygo_users;

-- Mensagem de confirmação
SELECT 'Databases whatygo_auth e whatygo_users criados com sucesso!' as message;
