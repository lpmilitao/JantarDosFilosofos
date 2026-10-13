#!/usr/bin/env bash
# Roda o experimento completo e monta a tabela comparativa.
#   V0, V2 (limites 1, 2 e 4) e V3: RUNS execuções cada (padrão 5)
#   V1: V1_RUNS execuções (padrão 10); o deadlock é probabilístico, o script conta quantas
#       travaram e guarda a primeira evidência em resultados/v1_deadlock.txt
#
# Uso:  ./run_all.sh            (5 execuções por versão, 10 da V1)
#       RUNS=10 V1_RUNS=20 ./run_all.sh
set -euo pipefail
cd "$(dirname "$0")"

RUNS="${RUNS:-5}"
mkdir -p bin resultados
rm -f resultados/brutos.csv resultados/tabela.md resultados/*.txt

echo "== Ambiente =="
cpu="$(lscpu 2>/dev/null | sed -n 's/^Model name:[[:space:]]*//p')" || true
[ -n "$cpu" ] || cpu="$(sysctl -n machdep.cpu.brand_string 2>/dev/null || echo '?')"
{
  echo "Go ............ $(go version)"
  echo "SO ............ $(uname -srm)"
  echo "Processador ... $cpu"
  echo "Cores lógicos . $(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo '?')"
} | tee resultados/ambiente.txt

echo "== Compilando =="
go build -o bin/v0 ./v0_sequencial
go build -o bin/v1 ./v1_deadlock
go build -o bin/v2 ./v2_semaforo
go build -o bin/v3 ./v3_ordenacao

# rodar <nome> <comando...>: executa RUNS vezes, guarda a saída e acumula a linha CSV
rodar() {
  local nome="$1"; shift
  for i in $(seq 1 "$RUNS"); do
    local arq="resultados/${nome}_${i}.txt"
    "$@" > "$arq"
    grep '^CSV,' "$arq" >> resultados/brutos.csv
    echo "-- $nome, execução $i/$RUNS: $(grep 'Tempo total' "$arq")"
  done
}

echo "== Executando (${RUNS} execuções por versão, grade T/E silenciada com -q) =="
rodar v0 ./bin/v0 -q
rodar v2_limite1 ./bin/v2 -limite 1 -q
rodar v2_limite2 ./bin/v2 -limite 2 -q
rodar v2_limite4 ./bin/v2 -limite 4 -q
rodar v3 ./bin/v3 -q

echo "== Tabela comparativa =="
awk -F, '
{
  v = $2
  if (!(v in n)) ordem[++k] = v
  n[v]++; t[v] += $3; t2[v] += $3 * $3; w[v] += $4
  if ($5 > mw[v]) mw[v] = $5
  if ($6 > mc[v]) mc[v] = $6
  c[v] = $7
}
END {
  print "| Versão | Execuções | Tempo total médio (s) | Desvio padrão (s) | Espera média (ms) | Maior espera (ms) | Máx. comendo juntos | Ciclos por filósofo |"
  print "|---|---|---|---|---|---|---|---|"
  for (i = 1; i <= k; i++) {
    v = ordem[i]; m = t[v] / n[v]; sd = 0
    if (n[v] > 1) { var = (t2[v] - n[v] * m * m) / (n[v] - 1); if (var > 0) sd = sqrt(var) }
    printf "| %s | %d | %.3f | %.3f | %.3f | %.3f | %d | %s |\n", v, n[v], m / 1000, sd / 1000, w[v] / n[v], mw[v], mc[v], c[v]
  }
}' resultados/brutos.csv | tee resultados/tabela.md

V1_RUNS="${V1_RUNS:-10}"
echo "== V1 (o deadlock é probabilístico: ${V1_RUNS} execuções) =="
travou=0
set +e
for i in $(seq 1 "$V1_RUNS"); do
  ./bin/v1 > "resultados/v1_exec_$i.txt" 2>&1
  codigo=$?   # 2 = o runtime do Go abortou por deadlock
  if [ "$codigo" -eq 2 ]; then
    travou=$((travou + 1))
    echo "-- V1, execução $i/$V1_RUNS: DEADLOCK"
  else
    echo "-- V1, execução $i/$V1_RUNS: completou ($(grep 'Tempo total' "resultados/v1_exec_$i.txt"))"
  fi
done
set -e
echo "V1: deadlock em $travou de $V1_RUNS execuções."

# guarda a primeira execução que travou (evidência) e a primeira que completou
trav="$(grep -l 'DEADLOCK DETECTADO' resultados/v1_exec_*.txt 2>/dev/null | head -1 || true)"
comp="$(grep -L 'DEADLOCK DETECTADO' resultados/v1_exec_*.txt 2>/dev/null | head -1 || true)"
if [ -n "$trav" ]; then
  cp "$trav" resultados/v1_deadlock.txt
  echo "Evidência do deadlock: resultados/v1_deadlock.txt (cópia de $trav)"
else
  echo "Nenhuma execução travou. Rode de novo ou force: ./bin/v1 -jitter 0   (trava sempre)"
fi
if [ -n "$comp" ]; then
  cp "$comp" resultados/v1_completou.txt
  echo "Execução que completou sem deadlock: resultados/v1_completou.txt"
fi
