// V1 - Versão concorrente sujeita a deadlock.
//
// Base: "Dining Philosophers: Five Forks and a Deadlock" (Rafiul Alam)
// https://alamrafiul.com/blogs/dining-philosophers-deadlock/
// Cada garfo é um sync.Mutex e todo filósofo pega primeiro o garfo esquerdo e depois o direito.
//
// Ajustes sobre o código base:
//   - 20 ciclos e tempos de pensar/comer iguais aos das outras versões;
//   - a pausa entre pegar o 1º e o 2º garfo (50 ms no original) virou a flag -atraso
//     (padrão 10 ms) e o tempo de pensar passou a ser sorteado em torno de -pensar
//     (flag -jitter, padrão 20 ms: de 10 a 30 ms, mesma média das outras versões). Com o
//     sorteio o deadlock deixa de ser certo. Nos testes (jitter 20 ms, 20 execuções cada),
//     atraso 2 ms travou 0 vezes, 5 ms 2, 8 ms 6, 10 ms 11, 15 ms 18 e 20 ms 20. Sempre
//     antes de alguém terminar o 1º ciclo, porque travar exige os 5 famintos ao mesmo tempo;
//   - para travar SEMPRE (demonstração) use -jitter 0 (todos pensam igual e acordam juntos)
//     ou -atraso 20ms. A flag -seed repete os tempos de pensar sorteados;
//   - a detecção por timeout com select/time.After foi trocada por um "vigia" que
//     declara deadlock quando nenhum filósofo progride por -timeout (padrão 2 s) e
//     imprime a EVIDÊNCIA: quem segura qual garfo, por qual garfo espera, de quem é
//     esse garfo e a espera circular;
//   - depois do relatório o vigia termina e a main continua em wg.Wait(), então o runtime
//     do Go aborta com "fatal error: all goroutines are asleep - deadlock!" e imprime a pilha
//     de cada goroutine (mais uma evidência para o relatório);
//   - imprime a mesma grade T/E da V0 (-q silencia); no deadlock a grade para depois dos T1,
//     porque ninguém consegue chegar ao E1;
//   - garfos numerados como na figura do enunciado (P1 usa F5 e F1; P2 usa F1 e F2; ...);
//   - ao final imprime as estatísticas de cada filósofo e as gerais.
//
// Se o programa completar sem deadlock, imprime as estatísticas normais. O tempo de -atraso
// entra na espera medida e o tempo de pensar sorteado não é o fixo das outras versões, então
// essa execução não deve entrar na tabela comparativa com V0, V2 e V3.
//
// Arquivo independente: não depende de nenhum outro pacote do projeto.
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
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
// os dois garfos em mãos.
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
// V1: pega o esquerdo, depois o direito, sem nenhuma ordem global -> deadlock possível
// ---------------------------------------------------------------------------

// Para o vigia: "progresso" = alguém pegou um garfo ou comeu/terminou de comer.
var progresso atomic.Int64

func marcarProgresso()           { progresso.Store(time.Now().UnixNano()) }
func ultimoProgresso() time.Time { return time.Unix(0, progresso.Load()) }

type filosofo struct {
	id       int
	esq, dir *garfo
	segura   atomic.Uint32 // máscara dos garfos que segura (só o próprio filósofo escreve)
	espera   atomic.Int32  // garfo pelo qual espera (-1 = nenhum)
	rng      *rand.Rand    // sorteia o tempo de pensar (só a goroutine do filósofo usa)
}

// tempoPensando sorteia o tempo de pensar em [pensar - jitter/2, pensar + jitter/2],
// que tem a mesma média das outras versões. Com jitter 0 é sempre cfg.pensar.
// Sem esse sorteio os cinco filósofos acordam juntos e o deadlock vira certeza.
func (f *filosofo) tempoPensando(jitter time.Duration) time.Duration {
	if jitter <= 0 {
		return cfg.pensar
	}
	t := cfg.pensar - jitter/2 + time.Duration(f.rng.Int63n(int64(jitter)+1))
	if t < 0 {
		t = 0
	}
	return t
}

func (f *filosofo) pegar(g *garfo) {
	f.espera.Store(int32(g.id))
	g.Lock()
	f.segura.Store(f.segura.Load() | 1<<uint(g.id))
	f.espera.Store(-1)
}

func (f *filosofo) jantar(atraso, jitter time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()

	for c := 1; c <= cfg.ciclos; c++ {
		// pensar
		grade(f.id, "T%d", c)
		time.Sleep(f.tempoPensando(jitter))

		// adquirir recursos: esquerdo, pausa, direito
		inicio := inicioEspera()
		f.pegar(f.esq)
		marcarProgresso()
		time.Sleep(atraso)
		f.pegar(f.dir)
		fimEspera(f.id, inicio)

		// comer
		comecaComer()
		grade(f.id, "E%d", c)
		marcarProgresso()
		time.Sleep(cfg.comer)
		terminaComer(f.id)
		marcarProgresso()

		// liberar recursos
		f.segura.Store(0)
		f.dir.Unlock()
		f.esq.Unlock()
	}
	concluiu(f.id)
}

// vigia declara deadlock quando ninguém progride por "limite" e imprime a evidência.
func vigia(filos []*filosofo, limite time.Duration, parar <-chan struct{}) {
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-parar:
			return
		case <-t.C:
			if parado := time.Since(ultimoProgresso()); parado > limite {
				relatarDeadlock(filos, parado)
				return
			}
		}
	}
}

func resumoCiclos() string {
	partes := make([]string, len(stats))
	for i := range partes {
		partes[i] = fmt.Sprintf("P%d=%d", i+1, stats[i].ciclos.Load())
	}
	return strings.Join(partes, " ")
}

func relatarDeadlock(filos []*filosofo, parado time.Duration) {
	n := len(filos)
	dono := func(g int) int {
		for _, f := range filos {
			if f.segura.Load()&(1<<uint(g)) != 0 {
				return f.id
			}
		}
		return -1
	}

	fmt.Printf("\n*** DEADLOCK DETECTADO: nenhum progresso há %s ***\n\n", parado.Round(time.Millisecond))
	fmt.Println("Filósofo | segura garfo | espera garfo | esse garfo está com")
	fmt.Println("---------+--------------+--------------+--------------------")
	for _, f := range filos {
		var seg []string
		for g := 0; g < n; g++ {
			if f.segura.Load()&(1<<uint(g)) != 0 {
				seg = append(seg, fmt.Sprintf("F%d", g+1))
			}
		}
		segura := "-"
		if len(seg) > 0 {
			segura = strings.Join(seg, ",")
		}
		espera, com := "-", "-"
		if w := int(f.espera.Load()); w >= 0 {
			espera = fmt.Sprintf("F%d", w+1)
			if d := dono(w); d >= 0 {
				com = fmt.Sprintf("P%d", d+1)
			}
		}
		fmt.Printf("P%-7d | %-12s | %-12s | %s\n", f.id+1, segura, espera, com)
	}

	// espera circular: segue "quem espera por quem" até repetir um filósofo
	proximo := func(p int) int {
		w := int(filos[p].espera.Load())
		if w < 0 {
			return -1
		}
		return dono(w)
	}
	for inicio := 0; inicio < n; inicio++ {
		visto := map[int]int{}
		var seq []int
		for p := inicio; p != -1; p = proximo(p) {
			if i, ok := visto[p]; ok {
				ciclo := seq[i:]
				partes := make([]string, 0, len(ciclo)+1)
				for _, q := range ciclo {
					partes = append(partes, fmt.Sprintf("P%d", q+1))
				}
				partes = append(partes, fmt.Sprintf("P%d", ciclo[0]+1))
				fmt.Printf("\nEspera circular: %s\n", strings.Join(partes, " espera por -> "))
				inicio = n // sai do laço externo
				break
			}
			visto[p] = len(seq)
			seq = append(seq, p)
		}
	}

	fmt.Println("\nCondições de Coffman presentes:")
	fmt.Println("  Exclusão mútua ........ cada garfo é um sync.Mutex: um dono por vez")
	fmt.Println("  Segura e espera ....... cada filósofo segura um garfo e espera pelo outro")
	fmt.Println("  Ausência de preempção . um Mutex não pode ser tomado à força do dono")
	fmt.Println("  Espera circular ....... ciclo mostrado acima")
	fmt.Printf("\nCiclos concluídos até o travamento: %s\n", resumoCiclos())
	fmt.Println("\n(Agora o runtime do Go deve abortar o programa: 'all goroutines are asleep - deadlock!')")
}

func main() {
	registrarFlags()
	atraso := flag.Duration("atraso", 10*time.Millisecond, "pausa entre pegar o 1º e o 2º garfo (0 = sem pausa; o deadlock quase não acontece)")
	jitter := flag.Duration("jitter", 20*time.Millisecond, "variação sorteada no tempo de pensar, em torno de -pensar (0 = todos pensam igual: com -atraso > 0 o deadlock é certo, já no ciclo 1)")
	semente := flag.Int64("seed", 0, "semente dos sorteios (0 = aleatória); repete os tempos de pensar, mas o escalonamento ainda varia")
	limite := flag.Duration("timeout", 2*time.Second, "tempo sem nenhum progresso para declarar deadlock")
	flag.Parse()
	validarFlags()
	stats = make([]estatistica, cfg.n)
	if *semente == 0 {
		*semente = time.Now().UnixNano()
	}

	garfos := novosGarfos(cfg.n)
	filos := make([]*filosofo, cfg.n)
	for i := range filos {
		e, d := indicesGarfos(i, cfg.n)
		filos[i] = &filosofo{id: i, esq: garfos[e], dir: garfos[d], rng: rand.New(rand.NewSource(*semente + int64(i)))}
		filos[i].espera.Store(-1)
	}

	if !cfg.silencioso {
		fmt.Printf("(atraso=%s jitter=%s seed=%d)\n", *atraso, *jitter, *semente)
	}
	cabecalho()
	marcarProgresso()

	var wg sync.WaitGroup
	parar := make(chan struct{})

	t0 = time.Now()
	for _, f := range filos {
		wg.Add(1)
		go f.jantar(*atraso, *jitter, &wg)
	}
	go vigia(filos, *limite, parar)

	wg.Wait() // em caso de deadlock, o runtime aborta aqui
	total := time.Since(t0)
	close(parar)

	fmt.Println("\nA V1 completou sem deadlock desta vez (rode de novo; para travar sempre use -jitter 0 ou -atraso 20ms).")
	relatorio("V1", total)
}
