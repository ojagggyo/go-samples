async function csvread(filename, tail, title, head) {
  console.log("csvread()", filename, tail, title, head);

  const params = new URLSearchParams();

  params.set("filename", filename);

  if (tail !== undefined) {
    params.set("tail", tail);
  }

  if (title !== undefined) {
    params.set("title", title);
  }

  if (head !== undefined) {
    params.set("head", head);
  }

  const response = await fetch(
    "/api/csv?" + params.toString()
  );

  if (!response.ok) {
    throw new Error("CSV API error: " + response.status);
  }

  const json = await response.json();

  const len = makeChart(json);
  afterMakeChart(len);
}
