function csvread(filename, lines, title) {
  window.getcsv = function(json) {
    makeChart(json);
  };

  const params = new URLSearchParams();
  params.set("filename", filename);

  if (lines !== undefined) params.set("lines", lines);
  if (title !== undefined) params.set("title", title);

  const script = document.createElement("script");
  script.src = "./api/getcsv?" + params.toString();
  script.id = "getcsv-script";

  script.onload = function() {
    script.remove();
  };

  script.onerror = function() {
    console.error("CSV APIの読み込みに失敗しました:", script.src);
    script.remove();
  };

  document.body.appendChild(script);
}
