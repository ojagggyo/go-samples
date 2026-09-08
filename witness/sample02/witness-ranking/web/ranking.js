const dev365 = [
  "cryptoking777",
  "dev.supporters",
  "enjoylondon",
  "hinomaru-jp",
  "hoasen",
  "inwi",
  "juddsmith079",
  "justyy",
  "maiyude",
  "matreshka",
  "menacamel",
  "parse",
  "rlawlstn123",
  "rnt1",
  "smt-wherein",
  "steem-agora",
  "steem-dragon",
  "steem.history",
  "steemchiller",
  "symbionts",
  "upeross",
];


let rankingChart = null;

/*
 * バー色判定用データ
 *
 * 最後のブロック生成から24時間以上経過したかを
 * リアルタイムに再判定するために使用する。
 */
let rankingColorContext = null;


/*
 * 経過時間・API更新の10分タイマー管理
 */
let elapsedUpdateTimer = null;
let top20ApiTimer = null;

/*
 * 上位Witness数
 * 通常20位 + 元の20位以内にあるDisabled数
 */
let topWitnessCount = 20;
let lowerApiTimer = null;
let stopAfter10MinutesTimer = null;
let elapsedDisplayEnabled = false;


/*
 * 現在のChart表示を更新
 */
function updateRankingChart() {
  if (!rankingChart) {
    return;
  }

  /*
   * 最後のブロック生成から24時間以上経過した場合、
   * バー色をリアルタイムに赤へ変更する。
   *
   * 色の優先順位:
   * 1. 自分のWitness
   * 2. 無効なSigning Key
   * 3. Version違い
   * 4. 最後のブロック生成から24時間以上
   * 5. 比較履歴なし
   * 6. MISS増加
   * 7. dev365
   * 8. 通常
   */
  if (rankingColorContext) {
    const {
      labels,
      running_version,
      signing_key,
      miss
    } = rankingColorContext;

    rankingChart.data.datasets[0].backgroundColor =
      labels.map(
        (label, index) => {
          /*
           * 1. 自分のWitness
           */
          if (label == user) {
            return "rgb(0 0 255)";
          }

          /*
           * 2. 無効なSigning Key
           */
          else if (
            signing_key[index] ==
            "STM1111111111111111111111111111111114T1Anm"
          ) {
            return "rgb(196 196 196)";
          }

          /*
           * 3. Versionが0.23.1以外
           */
          else if (
            running_version[index] !=
            "0.23.1"
          ) {
            return "rgb(0 0 0)";
          }

          /*
           * 4. 最後のブロック生成から24時間以上
           *
           * 上位WitnessはリアルタイムAPIの値を優先。
           * それ以外はCSVのLastBlockTimeを使用する。
           */
          let elapsedSeconds = null;

          const info =
            realtimeBlockTimes[label];

          if (info) {
            elapsedSeconds =
              info.elapsedSeconds +
              Math.floor(
                (
                  Date.now() -
                  info.fetchedAt
                ) /
                1000
              );
          }
          else if (
            rankingColorContext.last_block_time &&
            rankingColorContext.last_block_time[index]
          ) {
            elapsedSeconds =
              getCSVElapsedSeconds(
                rankingColorContext.last_block_time[index]
              );
          }

          if (
            Number.isFinite(elapsedSeconds) &&
            elapsedSeconds >= 24 * 60 * 60
          ) {
            return "rgb(255 0 0)";
          }

          /*
           * 5. 比較履歴なし
           */
          else if (
            miss[index] < 0
          ) {
            return "rgb(255 105 180)";
          }

          /*
           * 6. MISSが増加
           */
          else if (
            miss[index] > 0
          ) {
            return "rgb(255 165 0)";
          }

          /*
           * 7. dev365
           */
          else if (
            dev365.some(
              element =>
                element ==
                label
            )
          ) {
            return "rgb(54 181 221)";
          }

          /*
           * 8. 通常
           */
          return "rgb(51 221 204)";
        }
      );
  }

  rankingChart.update("none");
}


/*
 * すべての更新タイマーを停止
 */
function stopElapsedTimers() {
  if (elapsedUpdateTimer) {
    clearInterval(elapsedUpdateTimer);
    elapsedUpdateTimer = null;
  }

  if (top20ApiTimer) {
    clearInterval(top20ApiTimer);
    top20ApiTimer = null;
  }

  if (lowerApiTimer) {
    clearInterval(lowerApiTimer);
    lowerApiTimer = null;
  }

  if (stopAfter10MinutesTimer) {
    clearTimeout(stopAfter10MinutesTimer);
    stopAfter10MinutesTimer = null;
  }
}


/*
 * ボタン押下または初期表示時に、
 * 経過時間更新を10分間開始する。
 */
function startElapsedTimers() {

  stopElapsedTimers();

  elapsedDisplayEnabled = true;


  elapsedUpdateTimer = setInterval(
    updateRankingChart,
    1000
  );


  if (rankingChart) {

    const labels =
      rankingChart.data.labels;


    refreshTop20BlockTimes(
      labels.slice(0, topWitnessCount)
    );


    top20ApiTimer = setInterval(
      function () {
        if (rankingChart) {
          refreshTop20BlockTimes(
            rankingChart.data.labels.slice(0, topWitnessCount)
          );
        }
      },
      15000
    );


    refreshTop20BlockTimes(
      labels.slice(topWitnessCount, 100)
    );


    lowerApiTimer = setInterval(
      function () {
        if (rankingChart) {
          refreshTop20BlockTimes(
            rankingChart.data.labels.slice(topWitnessCount, 100)
          );
        }
      },
      60000
    );

  }


  stopAfter10MinutesTimer = setTimeout(
    function () {

      stopElapsedTimers();

      elapsedDisplayEnabled = false;

      updateRankingChart();

    },
    10 * 60 * 1000
  );

}


/*
 * 上位Witnessの
 * リアルタイム経過時間
 *
 * {
 *
 *   justyy: {
 *
 *     elapsedSeconds: 12,
 *     fetchedAt: 123456789
 *
 *   }
 *
 * }
 */
const realtimeBlockTimes = {};


/*
 * Witness 1件あたりの高さ
 */
const ROW_HEIGHT = 18;


/*
 * フォント設定
 *
 * Witness名      : 13px / 400
 * 通常文字       : 12px / 400
 * 凡例・注釈     : 14px / 600
 */
const FONT_FAMILY = "Meiryo";

const FONT_SIZE_NORMAL = 12;
const FONT_SIZE_WITNESS = 13;
const FONT_SIZE_LEGEND = 14;
const FONT_SIZE_ANNOTATION = 12;

const FONT_WEIGHT_NORMAL = 400;
const FONT_WEIGHT_MEDIUM = 500;
const FONT_WEIGHT_BOLD = 600;


/*
 * Chart上部などの余白
 */
const CHART_MARGIN = 50;


/*
 * Annotationラベル
 */
const commonLabel = (
  color,
  content
) => ({

  enabled: true,

  content: content,

  position: "top",

  font: {
    family: FONT_FAMILY,
    size: FONT_SIZE_LEGEND,
    weight: FONT_WEIGHT_BOLD,
  },

  color: color,

  backgroundColor: "white",

});


/*
 * Annotationライン
 */
const commonLine = (
  value,
  color
) => ({

  type: "line",

  scaleID: "y",

  value: value - 0.5,

  borderColor: color,

  borderWidth: 2,

});


/*
 * 対応Version
 *
 * 0.23.1
 * 0.23.2
 */
function isSupportedVersion(
  version
) {

  /*
   * CSVから読み込んだ値に
   * 空白などが含まれても正しく判定する。
   */
  const normalizedVersion =

    String(
      version
      ||
      ""
    )
      .trim()
      .replace(
        /^\(/,
        ""
      )
      .replace(
        /\)$/,
        ""
      );


  return (

    normalizedVersion === "0.23.1"

    ||

    normalizedVersion === "0.23.2"

  );

}


/*
 * 秒数を経過時間文字列へ変換
 *
 * 上位20:
 *
 * 🕒12s
 * 🕒45s
 * 🕒1m 02s
 *
 * 21位以下:
 *
 * 🕒12m
 * 🕒1h 20m
 * 🕒2d 3h
 */
function formatElapsedSeconds(
  totalSeconds,
  showSeconds
) {

  if (
    !Number.isFinite(
      totalSeconds
    )
    ||
    totalSeconds < 0
  ) {
    return "";
  }


  totalSeconds =
    Math.floor(
      totalSeconds
    );


  const days =
    Math.floor(
      totalSeconds /
      86400
    );


  const hours =
    Math.floor(
      (
        totalSeconds %
        86400
      )
      /
      3600
    );


  const minutes =
    Math.floor(
      (
        totalSeconds %
        3600
      )
      /
      60
    );


  const seconds =
    totalSeconds %
    60;


  /*
   * 上位Witness
   *
   * 秒まで表示
   */
  if (
    showSeconds
  ) {

    if (
      days > 0
    ) {

      return (
        "🕒"
        +
        days
        +
        "d "
        +
        hours
        +
        "h"
      );

    }


    if (
      hours > 0
    ) {

      return (
        "🕒"
        +
        hours
        +
        "h "
        +
        minutes
        +
        "m "
        +
        String(
          seconds
        ).padStart(
          2,
          "0"
        )
        +
        "s"
      );

    }


    if (
      minutes > 0
    ) {

      return (
        "🕒"
        +
        minutes
        +
        "m "
        +
        String(
          seconds
        ).padStart(
          2,
          "0"
        )
        +
        "s"
      );

    }


    return (
      "🕒"
      +
      seconds
      +
      "s"
    );

  }


  /*
   * 21位以下
   *
   * 秒なし
   */
  if (
    days > 0
  ) {

    return (
      "🕒"
      +
      days
      +
      "d"
      +
      (
        hours > 0

          ?

          " "
          +
          hours
          +
          "h"

          :

          ""

      )
    );

  }


  if (
    hours > 0
  ) {

    return (
      "🕒"
      +
      hours
      +
      "h"
      +
      (
        minutes > 0

          ?

          " "
          +
          minutes
          +
          "m"

          :

          ""

      )
    );

  }


  /*
   * 21～100位でも
   * 1分未満は秒で表示する。
   *
   * 例:
   * 🕒45s
   *
   * これで 🕒0m にならない。
   */
  if (
    minutes === 0
  ) {

    return (
      "🕒"
      +
      seconds
      +
      "s"
    );

  }


  return (
    "🕒"
    +
    minutes
    +
    "m"
  );

}


/*
 * CSVのLastBlockTimeから
 * 経過秒数を計算
 */
function getCSVElapsedSeconds(
  timestamp
) {

  if (
    !timestamp
  ) {
    return null;
  }


  const date =
    new Date(

      timestamp.endsWith(
        "Z"
      )

        ?

        timestamp

        :

        timestamp
        +
        "Z"

    );


  const time =
    date.getTime();


  if (
    !Number.isFinite(
      time
    )
  ) {
    return null;
  }


  const diffMs =
    Date.now()
    -
    time;


  if (
    diffMs < 0
  ) {
    return null;
  }


  return Math.floor(
    diffMs /
    1000
  );

}


/*
 * 上位Witnessの
 * 最新Block Timeを取得
 */
async function refreshTop20BlockTimes(
  labels
) {

  if (
    !labels
    ||
    labels.length === 0
  ) {
    return;
  }


  try {

    const response =
      await fetch(

        "./api/block-times?users="
        +
        encodeURIComponent(
          labels.join(
            ","
          )
        )

      );


    if (
      !response.ok
    ) {

      throw new Error(
        "API error: "
        +
        response.status
      );

    }


    const results =
      await response.json();


    const fetchedAt =
      Date.now();


    results.forEach(
      result => {

        /*
         * APIの返却形式に対応する。
         *
         * elapsed_seconds がある場合はそれを使用。
         * block_time の場合は現在時刻との差を秒数に変換する。
         */
        let elapsedSeconds =

          Number(
            result.elapsed_seconds
          );


        if (
          !Number.isFinite(
            elapsedSeconds
          )
          &&
          result.block_time
        ) {

          elapsedSeconds =

            getCSVElapsedSeconds(
              result.block_time
            );

        }


        /*
         * 有効な時刻が取得できた場合だけ保存する。
         *
         * APIから block_time が返る現在の形式でも、
         * NaNを保存して経過時間表示が消えることを防ぐ。
         */
        if (
          Number.isFinite(
            elapsedSeconds
          )
        ) {

          realtimeBlockTimes[
            result.name
          ] = {

            elapsedSeconds:
              elapsedSeconds,

            fetchedAt:
              fetchedAt

          };

        }

      }
    );


    /*
     * API取得直後に
     * 表示更新
     */
    if (
      rankingChart
    ) {

      rankingChart.update(
        "none"
      );

    }

  }

  catch (
  error
  ) {

    console.error(
      "block-times error:",
      error
    );

  }

}


/*
 * Chart作成
 */
function makeChart(
  json
) {


  console.log(
    "span:",
    span
  );


  console.log(
    "limit:",
    limit
  );


  const miss1 =
    "⚠️";


  const miss2 =
    "❌";


  const stat =
    "🚨";


  const change =
    "🌀";


  const now =
    new Date();


  const labels =
    [];


  const datas =
    [];


  const running_version =
    [];


  const signing_key =
    [];


  const last_update =
    [];


  const last_block_time =
    [];


  const miss =
    [];


  const signing_key_change =
    [];


  /*
   * elem[9]
   *
   * 前回の同じ単位期間での順位
   *
   * 0 または空:
   * 比較履歴なし
   */
  const previous_rank =
    [];


  /*
   * URLのlimit件だけ表示
   */
  const csv =
    json.csv.slice(
      0,
      limit
    );


  csv.forEach(
    function (
      elem
    ) {


      labels.push(
        elem[0]
      );


      datas.push(

        parseInt(
          elem[1]
          /
          1000000000000
        )

      );


      running_version.push(
        elem[2]
      );


      signing_key.push(
        elem[3]
      );


      last_update.push(

        (
          now
          -
          new Date(
            elem[5]
          )
        )
        /
        1000
        /
        3600
        -
        9

      );


      /*
       * elem[6]
       *
       * LastBlockTime
       */
      last_block_time.push(
        elem[6]
      );


      /*
       * elem[7]
       *
       * -1 = 比較履歴なし
       *  0 = MISSなし
       *  1以上 = MISSあり
       */
      miss.push(
        Number(
          elem[7]
        )
      );


      /*
       * elem[8]
       *
       * SigningKeyChange
       */
      signing_key_change.push(
        elem[8]
      );


      /*
       * elem[9]
       *
       * 前回の同じ単位期間での順位
       */
      previous_rank.push(
        Number(
          elem[9]
        )
      );


    }
  );


  /*
   * Witness件数に応じて
   * Chartの高さを変更
   */
  const chartHeight =

    labels.length
    *
    ROW_HEIGHT

    +

    CHART_MARGIN;


  document
    .getElementById(
      "chart-area"
    )
    .style.height =

    chartHeight
    +
    "px";


  /*
   * TOPライン
   *
   * 通常は20位まで。
   * 元の20位以内にDisabledがある場合は、
   * その数だけ21位以降も上位Witnessとして扱う。
   */
  const disabledSigningKey =
    "STM1111111111111111111111111111111114T1Anm";

  const disabledInBaseTop =
    signing_key
      .slice(0, 20)
      .filter(
        key =>
          key === disabledSigningKey
      )
      .length;

  topWitnessCount =
    20 +
    disabledInBaseTop;


  /*
   * バーの色
   *
   * 優先順位:
   *
   * 1. 自分のWitness       → 青
   * 2. Signing Keyが無効   → グレー
   * 3. Versionが0.23.1以外 → 黒
   * 4. 最後のブロック生成から24時間以上 → 赤
   * 5. 比較履歴なし         → ピンク
   * 6. MISSが増加           → オレンジ
   * 7. dev365               → 水色
   * 8. それ以外             → 緑
   */
  rankingColorContext = {
    labels:
      labels,
    running_version:
      running_version,
    signing_key:
      signing_key,
    miss:
      miss,
    last_block_time:
      last_block_time
  };

  const backgroundColors =
    labels.map(
      (label, index) => {
        /*
         * 1. 自分のWitness
         */
        if (
          label ==
          user
        ) {
          return "rgb(0 0 255)";
        }

        /*
         * 2. 無効なSigning Key
         */
        else if (
          signing_key[index] ==
          "STM1111111111111111111111111111111114T1Anm"
        ) {
          return "rgb(196 196 196)";
        }

        /*
         * 3. Versionが0.23.1以外
         */
        else if (
          running_version[index] !=
          "0.23.1"
        ) {
          return "rgb(0 0 0)";
        }

        /*
         * 4. 最後のブロック生成から24時間以上
         */
        let elapsedSeconds = null;

        const info =
          realtimeBlockTimes[label];

        if (info) {
          elapsedSeconds =
            info.elapsedSeconds +
            Math.floor(
              (
                Date.now() -
                info.fetchedAt
              ) /
              1000
            );
        }
        else {
          elapsedSeconds =
            getCSVElapsedSeconds(
              last_block_time[index]
            );
        }

        if (
          Number.isFinite(elapsedSeconds) &&
          elapsedSeconds >= 24 * 60 * 60
        ) {
          return "rgb(255 0 0)";
        }

        /*
         * 5. 比較履歴なし
         */
        else if (
          miss[index] < 0
        ) {
          return "rgb(255 105 180)";
        }

        /*
         * 6. MISSが増加
         */
        else if (
          miss[index] > 0
        ) {
          return "rgb(255 165 0)";
        }

        /*
         * 7. dev365
         */
        else if (
          dev365.some(
            element =>
              element ==
              label
          )
        ) {
          return "rgb(54 181 221)";
        }

        /*
         * 8. 通常
         */
        else {
          return "rgb(51 221 204)";
        }
      }
    );

  /*
   * Witness名の文字色
   */
  const fontColors_stat =

    datas.map(

      (
        value,
        index
      ) => {


        if (

          signing_key[index]

          ==

          "STM1111111111111111111111111111111114T1Anm"

        ) {

          return "rgb(196 196 196)";

        }


        return (

          last_update[index]

          <

          24

        )

          ?

          "rgb(0 0 0)"

          :

          "red";


      }

    );


  /*
   * MISS表示の文字色
   */
  const fontColors_miss =

    datas.map(

      (
        value,
        index
      ) => {


        if (

          signing_key[index]

          ==

          "STM1111111111111111111111111111111114T1Anm"

        ) {

          return "rgb(196 196 196)";

        }


        /*
         * 比較履歴なし
         */
        if (
          miss[index] < 0
        ) {

          return "rgb(255 105 180)";

        }


        /*
         * MISSあり
         */
        if (
          miss[index] > 0
        ) {

          return "rgb(255 0 0)";

        }


        return "rgb(0 0 0)";


      }

    );


  /*
   * Chartデータ
   */
  const data = {

    labels:
      labels,


    datasets: [

      {

        label:
          "Votes (MV)",


        backgroundColor:
          backgroundColors,


        data:
          datas,


        datalabels: {


          align:
            "end",


          anchor:
            "end",


          color:
            fontColors_miss,


          formatter:

            (
              value,
              context
            ) => {


              const missValue =

                miss[
                context.dataIndex
                ];


              const reason_msg =

                (

                  running_version[
                  context.dataIndex
                  ]

                  ==

                  "0.23.1"

                )

                  ?

                  ""

                  :

                  " ("

                  +

                  running_version[
                  context.dataIndex
                  ]

                  +

                  ")";


              /*
               * 比較履歴なし
               *
               * ピンク
               */
              if (
                missValue < 0
              ) {

                return (

                  value.toLocaleString()

                  +

                  " ⏳ No history"

                  +

                  reason_msg

                );

              }


              /*
               * MISSなし
               */
              if (
                missValue == 0
              ) {

                return (

                  value.toLocaleString()

                  +

                  reason_msg

                );

              }


              /*
               * MISSあり
               */
              const miss_msg =

                (

                  missValue > 10

                    ?

                    miss2

                    :

                    miss1

                )

                +

                missValue;


              return (

                value.toLocaleString()

                +

                miss_msg

                +

                " within 1 "

                +

                span

                +

                reason_msg

              );


            }

        }

      }

    ]

  };


  /*
   * Annotation
   */
  const annotations =
    [];


  /*
   * TOPライン
   */
  if (
    topWitnessCount <= labels.length
  ) {

    annotations.push({

      ...commonLine(
        topWitnessCount,
        "rgb(255 0 0)"
      ),


      label: {

        ...commonLabel(
          "red",
          "TOP "
          +
          topWitnessCount
        )

      }

    });

  }


  /*
   * Rank 100
   */
  if (
    100 <= labels.length
  ) {

    annotations.push({

      ...commonLine(
        100,
        "rgb(0 0 255)"
      ),


      label: {

        ...commonLabel(
          "blue",
          "Rank 100"
        )

      }

    });

  }


  /*
   * Rank 200
   */
  if (
    200 <= labels.length
  ) {

    annotations.push({

      ...commonLine(
        200,
        "rgb(0 0 255)"
      ),


      label: {

        ...commonLabel(
          "blue",
          "Rank 200"
        )

      }

    });

  }


  /*
   * Rank 300
   */
  if (
    300 <= labels.length
  ) {

    annotations.push({

      ...commonLine(
        300,
        "rgb(0 0 255)"
      ),


      label: {

        ...commonLabel(
          "blue",
          "Rank 300"
        )

      }

    });

  }


  /*
   * Chart設定
   *
   * 投票数は横棒グラフ
   */
  const config = {

    type:
      "bar",


    data:
      data,


    options: {


      indexAxis:
        "y",


      scales: {


        x: {

          beginAtZero:
            true,


          position:
            "top",


          ticks: {

            color:
              "black",

            font: {
              family: FONT_FAMILY,
              size: FONT_SIZE_NORMAL,
              weight: FONT_WEIGHT_NORMAL,
            }

          }

        },


        y: {


          ticks: {

            font: {
              family: FONT_FAMILY,
              size: FONT_SIZE_WITNESS,
              weight: FONT_WEIGHT_NORMAL,
            },


            /*
             * 全Witnessを表示
             */
            autoSkip:
              false,


            color:
              fontColors_stat,


            callback:

              function (
                value,
                index
              ) {


                const label =
                  labels[index];


                /*
                 * 前回の同じ単位期間との順位比較
                 *
                 * 現在順位:
                 *   index + 1
                 *
                 * 前回順位:
                 *   previous_rank[index]
                 *
                 * 数字が小さくなった = 順位上昇 = ↗️
                 * 数字が大きくなった = 順位下降 = ↘️
                 *
                 * 前回データがない場合は何も表示しない。
                 */
                const currentRank =
                  index
                  +
                  1;


                const previousRank =
                  previous_rank[
                  index
                  ];


                let rankChangeMsg =
                  "";


                if (

                  Number.isFinite(
                    previousRank
                  )

                  &&

                  previousRank > 0

                ) {

                  if (

                    currentRank < previousRank

                  ) {

                    rankChangeMsg =
                      "↗️";

                  }

                  else if (

                    currentRank > previousRank

                  ) {

                    rankChangeMsg =
                      "↘️";

                  }

                }


                /*
                 * 🚨は Signing Key が有効で、
                 * Version が 0.23.1 または 0.23.2 の場合だけ表示する。
                 */
                const normalizedSigningKey =

                  String(
                    signing_key[index]
                    ||
                    ""
                  )
                    .trim();


                const isDisabledSigningKey =

                  normalizedSigningKey

                  ===

                  "STM1111111111111111111111111111111114T1Anm";


                const isSupportedVersionForAlert =

                  isSupportedVersion(
                    running_version[index]
                  );


                const change_msg =

                  (

                    signing_key_change[index]

                    ===

                    "1"

                    &&

                    !isDisabledSigningKey

                    &&

                    isSupportedVersionForAlert

                  )

                    ?

                    change

                    :

                    "";


                /*
                 * 対応外Version
                 */
                const isUnsupportedVersion =

                  !isSupportedVersionForAlert;


                let blockTimeMsg =
                  "";


                /*
                 * 経過時間を計算する。
                 *
                 * 上位WitnessはリアルタイムAPIを優先。
                 * 21～100位は60秒ごとのAPI結果を使用する。
                 * API結果がまだない場合はCSVを使用する。
                 */
                if (

                  index < topWitnessCount

                  &&

                  realtimeBlockTimes[
                  label
                  ]

                ) {


                  const info =

                    realtimeBlockTimes[
                    label
                    ];


                  /*
                   * API取得後から
                   * 現在までの秒数を追加
                   */
                  const additionalSeconds =

                    Math.floor(

                      (

                        Date.now()

                        -

                        info.fetchedAt

                      )

                      /

                      1000

                    );


                  const elapsedSeconds =

                    info.elapsedSeconds

                    +

                    additionalSeconds;


                  blockTimeMsg =

                    formatElapsedSeconds(
                      elapsedSeconds,
                      true
                    );


                }

                else {


                  /*
                   * 21～100位
                   *
                   * 60秒ごとのAPI取得結果があれば
                   * その最新値を使用する。
                   *
                   * API取得前だけCSVのLastBlockTimeを使用する。
                   */
                  const info =

                    realtimeBlockTimes[
                    label
                    ];


                  if (
                    info
                  ) {

                    const additionalSeconds =

                      Math.floor(

                        (
                          Date.now()
                          -
                          info.fetchedAt
                        )
                        /
                        1000

                      );


                    blockTimeMsg =

                      formatElapsedSeconds(

                        info.elapsedSeconds
                        +
                        additionalSeconds,

                        false

                      );

                  }

                  else {


                    const elapsedSeconds =

                      getCSVElapsedSeconds(

                        last_block_time[
                        index
                        ]

                      );


                    if (
                      elapsedSeconds !== null
                    ) {

                      blockTimeMsg =

                        formatElapsedSeconds(

                          elapsedSeconds,

                          false

                        );

                    }

                  }


                }


                /*
                 * 非活性Signing Keyは経過時間を表示しない。
                 *
                 * 上位Witnessはバージョンに関係なく表示する。
                 * 21～100位は0.23.1 / 0.23.2だけ表示する。
                 *
                 * 計算処理とは分離して最後に判定する。
                 */
                if (

                  isDisabledSigningKey

                  ||

                  (

                    index >= topWitnessCount

                    &&

                    !isSupportedVersionForAlert

                  )

                ) {

                  blockTimeMsg =
                    "";

                }


                return (

                  (
                    rankChangeMsg
                      ?
                      rankChangeMsg
                      +
                      " "
                      :
                      ""
                  )

                  +

                  label

                  +

                  (

                    /*
                     * 🚨は、
                     *
                     * 1. 最終更新が24時間以上
                     * 2. Signing Key が有効
                     * 3. Version が 0.23.1 または 0.23.2
                     *
                     * の全条件を満たす場合だけ表示する。
                     */
                    last_update[index]

                      >=

                      24

                      &&

                      !isDisabledSigningKey

                      &&

                      isSupportedVersionForAlert

                      ?

                      stat

                      :

                      ""

                  )

                  +

                  (

                    elapsedDisplayEnabled

                      &&

                      blockTimeMsg

                      ?

                      " "
                      +
                      blockTimeMsg

                      :

                      ""

                  )

                  +

                  change_msg

                );


              }

          }

        }

      },


      responsive:
        true,


      maintainAspectRatio:
        false,


      plugins: {


        datalabels: {

          font: {
            family: FONT_FAMILY,
            size: FONT_SIZE_NORMAL,
            weight: FONT_WEIGHT_NORMAL,
          }

        },


        annotation: {

          annotations:
            annotations

        }

      }

    }

  };


  /*
   * Plugin登録
   */
  Chart.register(

    ChartDataLabels,

    window[
    "chartjs-plugin-annotation"
    ]

  );


  Chart.defaults.font.family =
    FONT_FAMILY;

  Chart.defaults.font.size =
    FONT_SIZE_NORMAL;

  Chart.defaults.font.weight =
    FONT_WEIGHT_NORMAL;


  /*
   * 前のChartを削除
   */
  if (
    rankingChart
  ) {

    rankingChart.destroy();

  }


  /*
   * 新しいChartを作成
   */
  rankingChart =

    new Chart(

      document.getElementById(
        "myChart"
      ),

      config

    );


  /*
   * 上位Witnessの
   * 最新Block Timeを取得
   */
  refreshTop20BlockTimes(

    labels.slice(
      0,
      20
    )

  );


  /*
   * 21～100位の最新Block Timeを取得
   */
  refreshTop20BlockTimes(

    labels.slice(
      20,
      100
    )

  );


  /*
   * ページ表示時から10分間、
   * 経過時間・API更新を開始
   */
  startElapsedTimers();


  /*
   * Chartクリック
   */
  document
    .getElementById(
      "myChart"
    )
    .onclick = function (
      evt
    ) {


      const p =

        rankingChart
          .getElementsAtEventForMode(

            evt,

            "nearest",

            {
              intersect:
                true
            },

            true

          );


      /*
       * バーをクリック
       */
      if (
        p.length
      ) {


        const url =

          "/witness-ranking/detail.html?user="

          +

          rankingChart
            .data
            .labels[
          p[0].index
          ]

          +

          "&rank="

          +

          (p[0].index + 1)

          +

          "&top="

          +

          topWitnessCount

          +

          "&hours="

          +

          hours

          +

          "&limit=" + limit + "&color="

          +

          encodeURIComponent(

            rankingChart
              .data
              .datasets[0]
              .backgroundColor[
            p[0].index
            ]

          );


        window.open(
          url,
          "ranking"
        );

      }


      /*
       * Witness名部分をクリック
       */
      else {


        const yAxis =
          rankingChart.scales.y;


        if (

          evt.offsetX >=
          yAxis.left

          &&

          evt.offsetX <=
          yAxis.right

          &&

          evt.offsetY >=
          yAxis.top

          &&

          evt.offsetY <=
          yAxis.bottom

        ) {


          const y_index =

            Math.round(

              yAxis.getValueForPixel(
                evt.offsetY
              )

            );


          if (

            y_index >= 0

            &&

            y_index <

            rankingChart
              .data
              .labels
              .length

          ) {


            const url =

              "/ah/#"

              +

              rankingChart
                .data
                .labels[
              y_index
              ];


            window.open(
              url,
              "ranking"
            );

          }

        }

      }


    };


  return labels.length;

}




/*
 * 期間切替ボタンが押されたら、
 * 10分タイマーをリセットして経過時間更新を再開する。
 */
document.addEventListener(
  "click",
  function (event) {

    const target =
      event.target.closest(
        "button, input[type='button'], input[type='submit']"
      );

    if (!target) {
      return;
    }


    /*
     * makeChart() によるChart作成直後に
     * startElapsedTimers() が実行されるため、
     * ここでは既存タイマーだけ先に停止する。
     */
    stopElapsedTimers();

    elapsedDisplayEnabled = true;

  },
  true
);
