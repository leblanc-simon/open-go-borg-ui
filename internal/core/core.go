// Package core est le cœur applicatif : il enchaîne les opérations que
// l'interface graphique et la ligne de commande déclenchent de la même façon.
//
// Il ne connaît ni Fyne ni le terminal. Il reçoit un Runner (AR-01), les
// stores dont il a besoin, et rend compte par des structures et des clés de
// traduction, jamais par des libellés.
package core
