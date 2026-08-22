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
 * Witness 1件あたりの高さ
 */
const ROW_HEIGHT = 25;


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
    size: 14,
    weight: "bold",
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


function makeChart(json) {


  console.log(
    "span:",
    span
  );


  console.log(
    "limit:",
    limit
  );


  const miss1 = "⚠️";

  const miss2 = "❌";

  const stat = "🚨";

  const change = "🌀";


  const now =
    new Date();


  const labels = [];

  const datas = [];

  const running_version = [];

  const signing_key = [];

  const last_update = [];

  const miss = [];

  const signing_key_change = [];


  /*
   * URLのlimit件だけ表示
   */
  const csv =
    json.csv.slice(
      0,
      limit
    );


  csv.forEach(
    function (elem) {


      labels.push(
        elem[0]
      );


      datas.push(

        parseInt(
          elem[1] /
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
          now -
          new Date(elem[5])
        ) /
        1000 /
        3600 -
        9

      );


      /*
       * elem[6]

       * -1 = 比較履歴なし
       *  0 = MISSなし
       *  1以上 = MISSあり
       */
      miss.push(
        Number(elem[6])
      );


      signing_key_change.push(
        elem[7]
      );

    }
  );


  /*
   * Witness件数に応じて
   * Chartの高さを変更
   */
  const chartHeight =

    labels.length *
    ROW_HEIGHT

    +

    CHART_MARGIN;


  document
    .getElementById(
      "chart-area"
    )
    .style.height =

    chartHeight +
    "px";


  /*
   * TOPライン
   */
  let top20 = 20;


  /*
   * バーの色
   */
  const backgroundColors =

    datas.map(
      (
        value,
        index
      ) => {


        /*
         * 自分
         *
         * 赤
         */
        if (
          labels[index] == user
        ) {

          return "rgb(255 0 0)";

        }


        /*
         * 無効なSigning Key
         *
         * グレー
         */
        else if (

          signing_key[index]

          ==

          "STM1111111111111111111111111111111114T1Anm"

        ) {


          if (
            index < 20
          ) {

            top20 =
              top20 + 1;

          }


          return "rgb(196 196 196)";

        }


        /*
         * Version違い
         *
         * 黒
         */
        else if (

          running_version[index]

          !=

          "0.23.1"

        ) {

          return "rgb(0 0 0)";

        }


        /*
         * 比較する過去データがない
         *
         * ピンク
         *
         * Miss = -1
         */
        else if (

          miss[index] < 0

        ) {

          return "rgb(255 105 180)";

        }


        /*
         * 指定期間内に
         * MISSが増えた
         *
         * オレンジ
         */
        else if (

          miss[index] > 0

        ) {

          return "rgb(255 165 0)";

        }


        /*
         * dev365
         *
         * 水色
         */
        else if (

          dev365.some(

            element =>

              element ==
              labels[index]

          )

        ) {

          return "rgb(54 181 221)";

        }


        /*
         * 通常
         *
         * 緑
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

          "darkred";

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

          return "darkred";

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

                  " (" +

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
  const annotations = [];


  /*
   * TOPライン
   */
  if (

    top20 <= labels.length

  ) {

    annotations.push({

      ...commonLine(
        top20,
        "rgb(255 0 0)"
      ),

      label: {

        ...commonLabel(
          "red",
          "TOP " + top20
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
              "black"

          }

        },


        y: {


          ticks: {


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


                const change_msg =

                  (

                    signing_key_change[index]

                    ===

                    "1"

                  )

                    ?

                    change

                    :

                    "";


                return (

                  label

                  +

                  (

                    last_update[index]
                      <
                      24

                      ?

                      ""

                      :

                      stat

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

            size:
              11

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
    "Meiryo";


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
   * Chartクリック
   */
  document
    .getElementById(
      "myChart"
    )
    .onclick = function (evt) {


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

          "detail.html?user="

          +

          rankingChart
            .data
            .labels[
          p[0].index
          ]

          +

          "&hours="

          +

          hours

          +

          "&color="

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