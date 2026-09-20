// Package middleware fornece um conjunto de middlewares padrão net/http
// (Logger, Recoverer, Timeout, RequestID, etc.) para uso com o router.
//
// Vive no mesmo módulo do core (não em um repo/módulo separado) porque,
// na fase atual do projeto, o acoplamento entre as mudanças no core e nos
// middlewares essenciais é alto o suficiente para que versionamento
// conjunto seja a escolha certa. Ver discussão sobre single-module vs
// multi-module no histórico do projeto — migrar para módulo separado no
// futuro é uma extração viável, não uma decisão definitiva.
package middleware
