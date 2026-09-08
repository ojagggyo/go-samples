/*
 * フォント設定
 *
 * 通常文字       : 12px / 400
 * Witness名      : 13px / 400
 * 軸ラベル       : 12px / 600
 * 凡例           : 14px / 600
 * ブロック時間差 : 12px / 400
 * ブロック数      : 10px / 400
 */
const FONT_FAMILY = "Meiryo, sans-serif";
const FONT_FAMILY_EMOJI =
  '"Segoe UI Emoji", "Apple Color Emoji", "Noto Color Emoji", sans-serif';

const FONT_SIZE_NORMAL = 12;
const FONT_SIZE_WITNESS = 13;
const FONT_SIZE_AXIS = 12;
const FONT_SIZE_LEGEND = 14;
const FONT_SIZE_INTERVAL = 12;
const FONT_SIZE_BLOCK_COUNT = 10;

const FONT_WEIGHT_NORMAL = 400;
const FONT_WEIGHT_BOLD = 600;

let historyChart = null;
let chartRequestID = 0;

const params =
  new URLSearchParams(
    window.location.search
  );

const user =
  params.get("user")
  ||
  "";

let currentSpan =
  params.get("hours")
  ||
  "hour";

const color =
  params.get("color")
  ||
  "rgb(51 221 204)";

const rank =
  Number(
    params.get("rank")
    ||
    0
  );

/*
 * 上位Witness数
 * 通常20位 + 元の20位以内にあるDisabled数
 * ranking.js から detail URL の top パラメータで渡される。
 */
const topWitnessCount =
  Number(
    params.get("top")
    ||
    20
  );

/*
 * 詳細画面からランキング画面へ戻る。
 *
 * 詳細画面を開いたときの
 * 表示件数(limit) と期間(hours) を維持する。
 */
function goBackToRanking() {
  const limit =
    params.get("limit") ||
    "21";

  const span =
    params.get("hours") ||
    "hour";

  const query =
    new URLSearchParams();

  query.set(
    "limit",
    limit
  );

  query.set(
    "span",
    span
  );

  window.location.href =
    "/witness-ranking?" +
    query.toString();
}

const spanNames = {
  hour: "1時間",
  day: "1日",
  week: "1週間",
  month: "1ヶ月",
  year: "1年"
};

/*
 * ブロック生成時刻をグラフ上に表示するプラグイン。
 * ブロック時刻は履歴ポイントと完全に同じ分とは限らないため、
 * 前後の履歴ポイントからX座標を補間して印を付ける。
 */
const blockMarkerPlugin = {
  id: "blockMarkers",

  afterDraw(chart) {
    const blocks = chart.$witnessBlocks || [];

    if (!blocks.length || !chart.chartArea) {
      return;
    }

    const labels = chart.data.labels || [];
    if (!labels.length) {
      return;
    }

    const xScale = chart.scales.x;
    if (!xScale) {
      return;
    }

    const labelTimes = labels.map(label => {
      const time = new Date(label).getTime();
      return Number.isNaN(time) ? null : time;
    });

    const ctx = chart.ctx;

    ctx.save();
    ctx.strokeStyle = "rgba(0, 0, 0, 0.45)";
    ctx.fillStyle = "rgba(0, 0, 0, 0.85)";
    ctx.lineWidth = 1;

    const sortedBlocks = blocks
      .map(block => ({
        ...block,
        blockTime: new Date(block.time).getTime()
      }))
      .filter(block => !Number.isNaN(block.blockTime))
      .sort((a, b) => a.blockTime - b.blockTime);

    const previousTimes = new Map();

    sortedBlocks.forEach((block, index) => {
      previousTimes.set(
        block.blockTime,
        index > 0
          ? sortedBlocks[index - 1].blockTime
          : null
      );
    });

    const positions = new Map();

    sortedBlocks.forEach(block => {
      if (block.before_range) {
        return;
      }

      const blockTime = block.blockTime;
      let index = -1;

      for (let i = 0; i < labelTimes.length - 1; i++) {
        const t1 = labelTimes[i];
        const t2 = labelTimes[i + 1];

        if (t1 === null || t2 === null) {
          continue;
        }

        if (blockTime >= t1 && blockTime <= t2) {
          const ratio =
            t2 === t1
              ? 0
              : (blockTime - t1) / (t2 - t1);

          index = i + ratio;
          break;
        }
      }

      if (index < 0) {
        let nearest = 0;
        let distance = Infinity;

        labelTimes.forEach((time, i) => {
          if (time === null) return;

          const d = Math.abs(time - blockTime);
          if (d < distance) {
            distance = d;
            nearest = i;
          }
        });

        index = nearest;
      }

      const x = xScale.getPixelForValue(index);

      if (!Number.isFinite(x)) {
        return;
      }

      const previousTime = previousTimes.get(blockTime);

      const intervalSeconds =
        previousTime === null
          ? null
          : Math.max(
              0,
              Math.round(
                (blockTime - previousTime) / 1000
              )
            );

      const pixel = Math.round(x);

      if (!positions.has(pixel)) {
        positions.set(pixel, {
          x,
          count: 1,
          intervalSeconds
        });
      } else {
        const position = positions.get(pixel);
        position.count++;
        position.intervalSeconds = intervalSeconds;
      }
    });

    function formatBlockInterval(seconds) {
      if (seconds === null) {
        return "";
      }

      seconds = Math.max(0, Math.floor(seconds));

      const hours = Math.floor(seconds / 3600);
      const minutes = Math.floor((seconds % 3600) / 60);
      const remainSeconds = seconds % 60;

      if (hours > 0) {
        return `${hours}h${String(minutes).padStart(2, "0")}m${String(remainSeconds).padStart(2, "0")}s`;
      }

      if (minutes > 0) {
        return `${minutes}m${String(remainSeconds).padStart(2, "0")}s`;
      }

      return `${remainSeconds}s`;
    }

    /*
     * ⛏️ + 差分を1セットとして作る。
     */
    const markerSets = [];

    positions.forEach(
      ({ x, count, intervalSeconds }) => {

        ctx.setLineDash([3, 3]);
        ctx.beginPath();
        ctx.moveTo(
          x,
          chart.chartArea.top + 10
        );
        ctx.lineTo(
          x,
          chart.chartArea.bottom
        );
        ctx.stroke();
        ctx.setLineDash([]);

        const intervalText =
          formatBlockInterval(
            intervalSeconds
          );

        /*
         * ⛏️は、前回ブロックとの差分時間がなくても表示する。
         *
         * これまでは intervalText が空の場合、markerSets に
         * 追加されなかったため、該当Witnessの最初のブロックでは
         * APIが正常にブロックを返していても ⛏️ が表示されなかった。
         */
        ctx.save();

        ctx.font =
          `14px ${FONT_FAMILY_EMOJI}`;

        const emojiWidth =
          ctx.measureText("⛏️").width;

        ctx.font =
          `${FONT_SIZE_INTERVAL}px ${FONT_FAMILY}`;

        const textWidth =
          intervalText
            ? ctx.measureText(intervalText).width
            : 0;

        markerSets.push({
          x,
          text: intervalText,
          width:
            emojiWidth
            +
            textWidth
            +
            10
        });

        ctx.restore();

        if (count > 1) {
          ctx.save();

          ctx.font =
            `${FONT_SIZE_BLOCK_COUNT}px ${FONT_FAMILY}`;

          ctx.textAlign = "center";
          ctx.textBaseline = "bottom";

          ctx.fillText(
            String(count),
            x,
            chart.chartArea.top - 2
          );

          ctx.restore();
        }
      }
    );

    /*
     * 差分を必ず表示する。
     *
     * X方向で重ならないよう、各行を空きのある行へ配置する。
     * 10段固定の巡回方式ではなく、実際の文字幅を考慮する。
     */
    ctx.save();

    const rows = [];
    const rowGap = 17;
    const firstRowY =
      chart.chartArea.top + 7;

    markerSets
      .sort((a, b) => a.x - b.x)
      .forEach(set => {

        let selectedRow = -1;

        for (
          let row = 0;
          row < rows.length;
          row++
        ) {

          const last =
            rows[row][
              rows[row].length - 1
            ];

          if (
            !last
            ||
            set.x
            -
            (
              last.x
              +
              last.width / 2
            )
            >=
            20
          ) {
            selectedRow = row;
            break;
          }
        }

        if (selectedRow < 0) {
          selectedRow = rows.length;
          rows.push([]);
        }

        rows[selectedRow].push(set);

        const y =
          firstRowY
          +
          selectedRow * rowGap;

        /*
         * グラフ上部からはみ出す場合でも、
         * 差分文字そのものは必ず描画する。
         */
        ctx.font =
          `14px ${FONT_FAMILY_EMOJI}`;

        ctx.textAlign = "center";
        ctx.textBaseline = "middle";

        ctx.fillText(
          "⛏️",
          set.x,
          y
        );

        ctx.font =
          `${FONT_SIZE_INTERVAL}px ${FONT_FAMILY}`;

        ctx.textAlign = "left";

        ctx.fillText(
          set.text,
          set.x + 13,
          y
        );
      });

    ctx.restore();
    ctx.restore();
  }
};
Chart.register(blockMarkerPlugin);

/*
 * Chart.js 全体の基本フォント
 */
Chart.defaults.font.family = FONT_FAMILY;
Chart.defaults.font.size = FONT_SIZE_NORMAL;
Chart.defaults.font.weight = FONT_WEIGHT_NORMAL;


document
  .getElementById(
    "title"
  )
  .textContent =
  user
  +
  " - "
  +
  (
    spanNames[currentSpan]
    ||
    currentSpan
  );

async function loadHistory() {

  const query =
    new URLSearchParams();

  query.set(
    "user",
    user
  );

  query.set(
    "span",
    currentSpan
  );

  const response =
    await fetch(
      "./api/witness-history?"
      +
      query.toString()
    );

  if (
    !response.ok
  ) {

    throw new Error(
      await response.text()
    );

  }

  return response.json();
}

function makeChart(
  json
) {

  if (historyChart) {
    historyChart.destroy();
    historyChart = null;
  }

  const points =
    json.points
    ||
    [];

  if (
    !points.length
  ) {

    document
      .getElementById(
        "chart-area"
      )
      .innerHTML =
      '<div id="message">履歴データがありません。</div>';

    return;

  }

  const blocks =
    json.blocks
    ||
    [];

  const labels =
    points.map(
      point =>
        point.time
    );

  const votes =
    points.map(
      point =>
        point.votes
    );

  const miss =
    points.map(
      point =>
        point.miss
    );

  /*
   * 固定ヘッダーとChart.jsの凡例が重ならないように、
   * チャート領域そのものに上側の余白を確保する。
   *
   * Chart.js の layout.padding.top だけでは、
   * canvas 自体が固定ヘッダーの下に入り込んだ場合に
   * 凡例が隠れるため、canvas の親要素を下げる。
   */
  const chartCanvas =
    document.getElementById(
      "historyChart"
    );

  const chartArea =
    document.getElementById(
      "chart-area"
    );

  /*
   * 固定ヘッダーの実高さだけをコンテンツの上余白にする。
   * これ以上の余白はチャート側に追加しない。
   */
  const detailHeader =
    document.getElementById(
      "detail-header"
    );

  const detailContent =
    document.getElementById(
      "detail-content"
    );

  if (detailHeader && detailContent) {
    detailContent.style.paddingTop =
      `${detailHeader.offsetHeight}px`;
  }

  if (chartArea) {
    chartArea.style.boxSizing =
      "border-box";

    chartArea.style.paddingTop =
      "0px";
  }

  historyChart = new Chart(
    chartCanvas,
    {
      data: {
        labels: labels,

        datasets: [

          {
            type: "line",

            label: "Votes (MV)",

            data: votes,

            borderColor:
              color,

            backgroundColor:
              color.replace(
                /^rgb\((\d+)\s+(\d+)\s+(\d+)\)$/,
                "rgba($1, $2, $3, 0.20)"
              ),

            fill:
              "origin",


            borderWidth:
              2,

            pointRadius:
              0,
            yAxisID:
              "yVotes"
          },

          {
            type: "line",

            label: "MISS",

            data: miss,

            yAxisID:
              "yMiss",

            borderColor:
              "red",

            backgroundColor:
              "rgba(255, 0, 0, 0.18)",

            tension:
              0.15,

            pointRadius:
              0,

            borderWidth:
              2
          }

        ]
      },

      options: {

        /*
         * 上部に固定された操作領域と凡例が重ならないように、
         * チャート内部の上側に余白を確保する。
         */
        layout: {
          padding: {
            top: 0
          }
        },

        plugins: {
          legend: {
            labels: {
              font: {
                family: FONT_FAMILY,
                size: FONT_SIZE_LEGEND,
                weight: FONT_WEIGHT_BOLD,
              }
            }
          },

          tooltip: {
            titleFont: {
              family: FONT_FAMILY,
              size: FONT_SIZE_NORMAL,
              weight: FONT_WEIGHT_BOLD,
            },

            bodyFont: {
              family: FONT_FAMILY,
              size: FONT_SIZE_NORMAL,
              weight: FONT_WEIGHT_NORMAL,
            }
          }
        },



        responsive:
          true,

        maintainAspectRatio:
          false,

        interaction: {
          mode:
            "index",

          intersect:
            false
        },

        scales: {

          x: {
            title: {
              display:
                true,

              text: "Time",

              font: {
                family: FONT_FAMILY,
                size: FONT_SIZE_AXIS,
                weight: FONT_WEIGHT_BOLD,
              }
            },

            ticks: {
              font: {
                family: FONT_FAMILY,
                size: FONT_SIZE_NORMAL,
                weight: FONT_WEIGHT_NORMAL,
              },

              callback: function (value, index) {
                const label = this.getLabelForValue(value);
                const d = new Date(label);

                if (Number.isNaN(d.getTime())) {
                  return label;
                }

                const pad = n => String(n).padStart(2, "0");

                if (
                  currentSpan === "hour" ||
                  currentSpan === "day"
                ) {
                  const time =
                    pad(d.getHours()) +
                    ":" +
                    pad(d.getMinutes());

                  // 日付が変わった最初の目盛りだけ YYYY-MM-DD を表示
                  if (
                    index === 0 ||
                    new Date(
                      this.getLabelForValue(
                        this.getTicks().map(t => t.value)[index - 1]
                      )
                    ).toDateString() !== d.toDateString()
                  ) {
                    return (
                      d.getFullYear() +
                      "-" +
                      pad(d.getMonth() + 1) +
                      "-" +
                      pad(d.getDate()) +
                      "\n" +
                      time
                    );
                  }

                  return time;
                }

                if (false) {
                  const time =
                    pad(d.getHours()) +
                    ":" +
                    pad(d.getMinutes());

                  // 日付が変わった最初の目盛りだけ YYYY-MM-DD を表示
                  if (
                    index === 0 ||
                    new Date(
                      this.getLabelForValue(
                        this.getTicks().map(t => t.value)[index - 1]
                      )
                    ).toDateString() !== d.toDateString()
                  ) {
                    return (
                      d.getFullYear() +
                      "-" +
                      pad(d.getMonth() + 1) +
                      "-" +
                      pad(d.getDate()) +
                      "\n" +
                      time
                    );
                  }

                  return time;
                }

                const date =
                  d.getFullYear() +
                  "-" +
                  pad(d.getMonth() + 1) +
                  "-" +
                  pad(d.getDate());

                // 1週間～は同じ日付を繰り返し表示しない。
                // 日付が変わった最初の目盛りだけ表示する。
                if (
                  index === 0 ||
                  new Date(
                    this.getLabelForValue(
                      this.getTicks().map(t => t.value)[index - 1]
                    )
                  ).toDateString() !== d.toDateString()
                ) {
                  return date;
                }

                return "";
              }
            },

          },

          yVotes: {
            type:
              "linear",

            position:
              "left",

            beginAtZero:
              false,

            title: {
              display:
                true,

              text: "Votes (MV)",

              font: {
                family: FONT_FAMILY,
                size: FONT_SIZE_AXIS,
                weight: FONT_WEIGHT_BOLD,
              }
            }
          },

          yMiss: {
            type:
              "linear",

            position:
              "right",

            beginAtZero:
              true,

            title: {
              display:
                true,

              text: "MISS",

              font: {
                family: FONT_FAMILY,
                size: FONT_SIZE_AXIS,
                weight: FONT_WEIGHT_BOLD,
              }
            },

            grid: {
              drawOnChartArea:
                false
            },

            ticks: {
              precision:
                0,

              font: {
                family: FONT_FAMILY,
                size: FONT_SIZE_NORMAL,
                weight: FONT_WEIGHT_NORMAL,
              }
            }
          }

        }

      }
    }
  );

  updateBlockMarkers(blocks);
}

function updateBlockMarkers(blocks) {
  if (!historyChart) {
    return;
  }

  historyChart.$witnessBlocks = blocks || [];
  historyChart.update("none");
}

function shouldShowBlocksForSpan(span) {
  /*
   * ⛏️表示ルール
   *
   * 上位Witness:
   *   1時間 → 表示
   *   1日以降 → 非表示
   *
   * 上位Witness以外:
   *   1時間・1日 → 表示
   *   1週間以降 → 非表示
   */
  const isTopWitness =
    rank >= 1
    &&
    rank <= topWitnessCount;

  return isTopWitness
    ? span == "hour"
    : (
        span == "hour"
        ||
        span == "day"
      );
}

async function loadBlocks(span) {
  const query = new URLSearchParams();
  query.set("user", user);
  query.set("span", span);

  const response = await fetch(
    "./api/witness-blocks?" + query.toString()
  );

  if (!response.ok) {
    throw new Error(await response.text());
  }

  return response.json();
}

async function loadBlocksIfNeeded() {
  const shouldShowBlocks =
    shouldShowBlocksForSpan(currentSpan);

  if (!shouldShowBlocks) {
    return { blocks: [] };
  }

  return loadBlocks(currentSpan);
}

async function loadAndDraw() {
  const requestID = ++chartRequestID;
  const json = await loadHistory();

  if (requestID != chartRequestID) {
    return;
  }

  json.blocks = [];
  makeChart(json);

  try {
    const blockJson = await loadBlocksIfNeeded();

    if (requestID != chartRequestID) {
      return;
    }

    updateBlockMarkers(blockJson.blocks);
  }
  catch (error) {
    console.error("ブロック履歴の取得に失敗しました。", error);
  }
}

loadAndDraw()
  .catch(
    error => {

      console.error(error);

      document
        .getElementById("chart-area")
        .innerHTML =
        '<div id="message">履歴の取得に失敗しました。</div>';

    }
  );


/*
 * 期間切り換え
 */
document
  .querySelectorAll(
    "#periods button"
  )
  .forEach(
    button => {
      button.addEventListener(
        "click",
        async function () {

          const selectedSpan =
            this.dataset.span;

          const requestID = ++chartRequestID;

          currentSpan =
            selectedSpan;

          document
            .querySelectorAll(
              "#periods button"
            )
            .forEach(
              b =>
                b.classList.remove(
                  "active"
                )
            );

          this.classList.add(
            "active"
          );

          const query =
            new URLSearchParams();

          query.set(
            "user",
            user
          );

          query.set(
            "span",
            selectedSpan
          );

          try {

            const response =
              await fetch(
                "./api/witness-history?"
                +
                query.toString()
              );

            if (
              !response.ok
            ) {
              throw new Error(
                await response.text()
              );
            }

            const json =
              await response.json();

            if (requestID != chartRequestID) {
              return;
            }

            json.blocks = [];

            document
              .getElementById(
                "title"
              )
              .textContent =
              user
              +
              " - "
              +
              (
                spanNames[
                selectedSpan
                ]
                ||
                selectedSpan
              );

            makeChart(
              json
            );

            if (
              shouldShowBlocksForSpan(selectedSpan)
            ) {
              try {
                const blockJson =
                  await loadBlocks(selectedSpan);

                if (requestID != chartRequestID) {
                  return;
                }

                updateBlockMarkers(blockJson.blocks);
              }
              catch (error) {
                console.error(
                  "ブロック履歴の取得に失敗しました。",
                  error
                );
              }
            }

          }
          catch (
          error
          ) {

            console.error(
              error
            );

            document
              .getElementById(
                "chart-area"
              )
              .innerHTML =
              '<div id="message">履歴の取得に失敗しました。</div>';

          }

        }
      );
    }
  );

/*
 * 初期表示の期間を選択状態にする
 */
const initialButton =
  document.querySelector(
    '#periods button[data-currentSpan="' +
    currentSpan +
    '"]'
  );

if (
  initialButton
) {
  initialButton.classList.add(
    "active"
  );
}

//
