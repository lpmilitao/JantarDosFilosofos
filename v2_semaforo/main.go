// V2 - Sem deadlock, mas com sincronização excessiva.
//
// Base: "Dining Philosophers: The Waiter Solution" (Rafiul Alam)
// https://alamrafiul.com/blogs/dining-philosophers-waiter/
// Um "garçom" (semáforo feito com canal com buffer, já que o Go não tem semáforo nativo)
// limita quantos filósofos podem tentar pegar garfos ao mesmo tempo. Com limite < N
// não há deadlock (a ideia também aparece na página de Dijkstra/Tanenbaum do enunciado).
//
// Ajustes sobre o código base:
//   - o limite do garçom é a flag -limite e o PADRÃO É 1 (no artigo é 4). Com limite 1
//     só um filósofo por vez adquire, come e libera, o que serializa o "comer" e é a
//     sincronização excessiva pedida na V2. O artigo sugere testar 3, 2 e 1; o
//     run_all.sh roda 1, 2 e 4 para mostrar o efeito do grau de concorrência;
//   - 20 ciclos e tempos de pensar/comer iguais aos das outras versões (no original
//     eram 3 ciclos e tempos diferentes por filósofo);
//   - os Printf do original viraram a grade T/E da V0 (-q silencia) e o timeout de 10 s
//     foi removido;
//   - ao final imprime as estatísticas de cada filósofo e as gerais;
//   - tempo medido com time.Now()/time.Since(); garfos numerados como na figura do enunciado.
//
// Arquivo independente: não depende de nenhum outro pacote do projeto.
package main

import (
	"flag"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Parâmetros do experimento (iguais em V0, V1, V2 e V3)
// ---------------------------------------------------------------------------

var cfg struct {
	n          int           // número de filósofos
	ciclos     int           // ciclos por filósofo: pensar -> adquirir -> comer -> liberar
	pensar     time.Duration // tempo simulado pensando
	comer      time.Duration // tempo simulado comendo
	silencioso bool          // não imprime a grade T/E
}

func registrarFlags() {
	flag.IntVar(&cfg.n, "n", 5, "número de filósofos")
	flag.IntVar(&cfg.ciclos, "ciclos", 20, "ciclos por filósofo")
	flag.DurationVar(&cfg.pensar, "pensar", 20*time.Millisecond, "tempo simulado pensando")
	flag.DurationVar(&cfg.comer, "comer", 30*time.Millisecond, "tempo simulado comendo")
	flag.BoolVar(&cfg.silencioso, "q", false, "silencioso: não imprime a grade T/E (usado nas medições)")
}

func validarFlags() {
	if cfg.n < 2 || cfg.n > 32 {
		log.Fatalf("n deve estar entre 2 e 32 (recebido %d)", cfg.n)
	}
	if cfg.ciclos < 1 {
		log.Fatalf("ciclos deve ser >= 1 (recebido %d)", cfg.ciclos)
	}
}

// ---------------------------------------------------------------------------
// Grade de saída: uma coluna por filósofo, " T3" = pensando (ciclo 3), " E3" = comendo
// ---------------------------------------------------------------------------

func cabecalho() {
	if cfg.silencioso {
		return
	}
	cols := make([]string, cfg.n)
	for i := range cols {
		cols[i] = fmt.Sprintf("[P%d]", i+1)
	}
	fmt.Printf("\n%s\n\n", strings.Join(cols, " "))
}

func grade(id int, formato string, a ...any) {
	if cfg.silencioso {
		return
	}
	fmt.Printf("%s %s\n", strings.Repeat(" ", id*6), fmt.Sprintf(formato, a...))
}

// ---------------------------------------------------------------------------
// Estatísticas. "Espera" = tempo entre querer comer (terminou de pensar) e já ter
// os dois garfos em mãos (aqui inclui a espera pela permissão do garçom).
// ---------------------------------------------------------------------------

type estatistica struct {
	ciclos      atomic.Int64 // ciclos concluídos
	esperas     atomic.Int64 // quantas esperas foram medidas
	esperaTotal atomic.Int64 // soma das esperas (ns)
	esperaMax   atomic.Int64 // maior espera (ns)
	concluiuEm  atomic.Int64 // instante (ns desde o início) em que terminou o último ciclo
}

var (
	stats      []estatistica
	comendo    atomic.Int64
	maxComendo atomic.Int64
	t0         time.Time // início do jantar
)

func atualizaMax(a *atomic.Int64, v int64) {
	for {
		atual := a.Load()
		if v <= atual || a.CompareAndSwap(atual, v) {
			return
		}
	}
}

func inicioEspera() time.Time { return time.Now() }

func fimEspera(id int, inicio time.Time) {
	d := int64(time.Since(inicio))
	e := &stats[id]
	e.esperaTotal.Add(d)
	e.esperas.Add(1)
	atualizaMax(&e.esperaMax, d)
}

func comecaComer() { atualizaMax(&maxComendo, comendo.Add(1)) }

func terminaComer(id int) {
	comendo.Add(-1)
	stats[id].ciclos.Add(1)
}

func concluiu(id int) { stats[id].concluiuEm.Store(int64(time.Since(t0))) }

func ms(ns float64) float64 { return ns / 1e6 }

// relatorio imprime o resumo, a tabela por filósofo e uma linha "CSV,..." usada pelo run_all.sh.
func relatorio(versao string, total time.Duration) {
	fmt.Printf("\n==== %s ====\n", versao)
	fmt.Printf("Tempo total ................... %s\n", total)
	fmt.Printf("Máx. filósofos comendo juntos . %d\n", maxComendo.Load())

	fmt.Println("\nEstatísticas por filósofo:")
	fmt.Println("Filósofo | Ciclos | Espera média (ms) | Maior espera (ms) | Concluiu em (s)")
	fmt.Println("---------+--------+-------------------+-------------------+----------------")
	var somaEspera, qtd, picoEspera, somaCiclos, ultimo int64
	ciclosCSV := make([]string, len(stats))
	for i := range stats {
		e := &stats[i]
		n, soma, pico, fim := e.esperas.Load(), e.esperaTotal.Load(), e.esperaMax.Load(), e.concluiuEm.Load()
		media := 0.0
		if n > 0 {
			media = float64(soma) / float64(n)
		}
		fmt.Printf("P%-7d | %6d | %17.3f | %17.3f | %15.3f\n",
			i+1, e.ciclos.Load(), ms(media), ms(float64(pico)), float64(fim)/1e9)
		somaEspera += soma
		qtd += n
		somaCiclos += e.ciclos.Load()
		if pico > picoEspera {
			picoEspera = pico
		}
		if fim > ultimo {
			ultimo = fim
		}
		ciclosCSV[i] = fmt.Sprint(e.ciclos.Load())
	}
	mediaGeral := 0.0
	if qtd > 0 {
		mediaGeral = float64(somaEspera) / float64(qtd)
	}
	fmt.Println("---------+--------+-------------------+-------------------+----------------")
	fmt.Printf("Geral    | %6d | %17.3f | %17.3f | %15.3f\n",
		somaCiclos, ms(mediaGeral), ms(float64(picoEspera)), float64(ultimo)/1e9)

	fmt.Printf("CSV,%s,%.3f,%.3f,%.3f,%d,%s\n",
		versao, ms(float64(total)), ms(mediaGeral), ms(float64(picoEspera)),
		maxComendo.Load(), strings.Join(ciclosCSV, "/"))
}

// ---------------------------------------------------------------------------
// Garfos
// ---------------------------------------------------------------------------

type garfo struct {
	id int
	sync.Mutex
}

func novosGarfos(n int) []*garfo {
	g := make([]*garfo, n)
	for i := range g {
		g[i] = &garfo{id: i}
	}
	return g
}

// indicesGarfos devolve os garfos (esquerdo, direito) do filósofo i, como na figura do
// enunciado: o garfo k fica à direita do filósofo k. Exibidos 1-based: P1 usa F5 (esq) e
// F1 (dir); P2 usa F1 e F2; ...; P5 usa F4 e F5.
func indicesGarfos(i, n int) (esq, dir int) { return (i + n - 1) % n, i }

// ---------------------------------------------------------------------------
// V2: garçom (semáforo contador) + garfos
// ---------------------------------------------------------------------------

// garcom é um semáforo contador: o canal com buffer tem "limite" vagas.
type garcom struct{ vagas chan struct{} }

func novoGarcom(limite int) *garcom { return &garcom{vagas: make(chan struct{}, limite)} }

func (g *garcom) pedir()   { g.vagas <- struct{}{} } // bloqueia se não há vaga
func (g *garcom) liberar() { <-g.vagas }

func jantar(id int, esq, dir *garfo, g *garcom, wg *sync.WaitGroup) {
	defer wg.Done()

	for c := 1; c <= cfg.ciclos; c++ {
		// pensar
		grade(id, "T%d", c)
		time.Sleep(cfg.pensar)

		// adquirir recursos: permissão do garçom, depois os dois garfos
		inicio := inicioEspera()
		g.pedir()
		esq.Lock()
		dir.Lock()
		fimEspera(id, inicio)

		// comer
		comecaComer()
		grade(id, "E%d", c)
		time.Sleep(cfg.comer)
		terminaComer(id)

		// liberar recursos
		dir.Unlock()
		esq.Unlock()
		g.liberar()
	}
	concluiu(id)
}

func main() {
	registrarFlags()
	limite := flag.Int("limite", 1, "filósofos que o garçom deixa tentar pegar garfos ao mesmo tempo (1 = sincronização máxima)")
	flag.Parse()
	validarFlags()
	if *limite < 1 || *limite >= cfg.n {
		log.Fatalf("limite deve estar entre 1 e n-1 (%d); com limite >= n o deadlock volta a ser possível", cfg.n-1)
	}
	stats = make([]estatistica, cfg.n)

	garfos := novosGarfos(cfg.n)
	g := novoGarcom(*limite)

	cabecalho()

	var wg sync.WaitGroup
	t0 = time.Now()
	for i := 0; i < cfg.n; i++ {
		e, d := indicesGarfos(i, cfg.n)
		wg.Add(1)
		go jantar(i, garfos[e], garfos[d], g, &wg)
	}
	wg.Wait()
	total := time.Since(t0)

	relatorio(fmt.Sprintf("V2 (limite=%d)", *limite), total)
}
