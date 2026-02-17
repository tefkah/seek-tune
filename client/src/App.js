import { useEffect, useState, useCallback } from "react";
import { ToastContainer, toast, Slide } from "react-toastify";
import "react-toastify/dist/ReactToastify.css";

const server = process.env.REACT_APP_BACKEND_URL || "http://localhost:5000";

function App() {
  const [stats, setStats] = useState({ totalEntries: 0, totalFingerprints: 0, storageEstimate: "0 B" });
  const [indexFile, setIndexFile] = useState(null);
  const [matchFile, setMatchFile] = useState(null);
  const [title, setTitle] = useState("");
  const [author, setAuthor] = useState("");
  const [matches, setMatches] = useState([]);
  const [indexing, setIndexing] = useState(false);
  const [matching, setMatching] = useState(false);
  const [lastIndex, setLastIndex] = useState(null);

  const fetchStats = useCallback(() => {
    fetch(`${server}/api/stats`)
      .then((r) => r.json())
      .then(setStats)
      .catch(() => {});
  }, []);

  useEffect(() => {
    fetchStats();
    const id = setInterval(fetchStats, 10000);
    return () => clearInterval(id);
  }, [fetchStats]);

  async function handleIndex(e) {
    e.preventDefault();
    if (!indexFile) {
      toast.error("select a file to index");
      return;
    }

    setIndexing(true);
    setLastIndex(null);

    const form = new FormData();
    form.append("file", indexFile);
    if (title) form.append("title", title);
    if (author) form.append("author", author);

    try {
      const resp = await fetch(`${server}/api/index`, { method: "POST", body: form });
      const data = await resp.json();
      if (!resp.ok) {
        toast.error(data.error || "indexing failed");
        return;
      }
      setLastIndex(data);
      toast.success(`indexed "${data.title}" by "${data.author}"`);
      setIndexFile(null);
      setTitle("");
      setAuthor("");
      fetchStats();
    } catch (_) {
      toast.error("network error");
    } finally {
      setIndexing(false);
    }
  }

  async function handleMatch(e) {
    e.preventDefault();
    if (!matchFile) {
      toast.error("select a file to match");
      return;
    }

    setMatching(true);
    setMatches([]);

    const form = new FormData();
    form.append("file", matchFile);

    try {
      const resp = await fetch(`${server}/api/match`, { method: "POST", body: form });
      const data = await resp.json();
      if (!resp.ok) {
        toast.error(data.error || "matching failed");
        return;
      }
      if (!data.matches || data.matches.length === 0) {
        toast.info("no matches found");
        return;
      }
      setMatches(data.matches);
      toast.success(`found ${data.matches.length} match(es) in ${data.searchTimeMs}ms`);
    } catch (_) {
      toast.error("network error");
    } finally {
      setMatching(false);
    }
  }

  return (
    <div className="App">
      <div className="TopHeader">
        <h2 style={{ color: "#374151" }}>SeekTune</h2>
        <div style={{ textAlign: "right", fontSize: "0.85rem", color: "#6b7280" }}>
          <div><strong>{stats.totalEntries}</strong> entries indexed</div>
          <div>{stats.totalFingerprints.toLocaleString()} fingerprints ({stats.storageEstimate})</div>
        </div>
      </div>

      <section style={{ marginBottom: "2rem" }}>
        <h3>Index Audio</h3>
        <p style={{ color: "#6b7280", fontSize: "0.9rem", marginBottom: "1rem" }}>
          Upload an audio file to add it to the fingerprint database. Supports any format ffmpeg can handle.
        </p>
        <form onSubmit={handleIndex}>
          <div style={{ display: "flex", gap: "12px", marginBottom: "0.5rem", flexWrap: "wrap" }}>
            <input
              type="text"
              placeholder="Title (optional, extracted from metadata)"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              style={{ flex: 1, minWidth: "200px" }}
            />
            <input
              type="text"
              placeholder="Author (optional)"
              value={author}
              onChange={(e) => setAuthor(e.target.value)}
              style={{ flex: 1, minWidth: "200px" }}
            />
          </div>
          <div style={{ display: "flex", gap: "12px", alignItems: "center", flexWrap: "wrap" }}>
            <input
              type="file"
              accept="audio/*"
              onChange={(e) => setIndexFile(e.target.files[0] || null)}
              style={{ flex: 1 }}
            />
            <button type="submit" disabled={indexing || !indexFile}>
              {indexing ? "Indexing..." : "Upload & Index"}
            </button>
          </div>
        </form>
        {lastIndex && (
          <div style={{
            marginTop: "1rem",
            padding: "0.75rem 1rem",
            background: "#f0fdf4",
            border: "1px solid #bbf7d0",
            borderRadius: "4px",
            fontSize: "0.9rem"
          }}>
            <strong>{lastIndex.title}</strong> by {lastIndex.author}
            <span style={{ marginLeft: "1rem", color: "#6b7280" }}>
              {lastIndex.fingerprints.toLocaleString()} fingerprints, ~{lastIndex.storageEstimate} in DB
              {lastIndex.durationSec > 0 && ` (${Math.round(lastIndex.durationSec / 60)} min)`}
            </span>
          </div>
        )}
      </section>

      <section style={{ marginBottom: "2rem" }}>
        <h3>Find Matches</h3>
        <p style={{ color: "#6b7280", fontSize: "0.9rem", marginBottom: "1rem" }}>
          Upload an audio file to search for matches in the database.
        </p>
        <form onSubmit={handleMatch}>
          <div style={{ display: "flex", gap: "12px", alignItems: "center", flexWrap: "wrap" }}>
            <input
              type="file"
              accept="audio/*"
              onChange={(e) => setMatchFile(e.target.files[0] || null)}
              style={{ flex: 1 }}
            />
            <button type="submit" disabled={matching || !matchFile}>
              {matching ? "Matching..." : "Find Matches"}
            </button>
          </div>
        </form>
        {matches.length > 0 && (
          <div style={{ marginTop: "1rem" }}>
            <table>
              <thead>
                <tr>
                  <th style={{ width: "40px" }}>#</th>
                  <th>Title</th>
                  <th>Author</th>
                  <th style={{ textAlign: "right" }}>Score</th>
                </tr>
              </thead>
              <tbody>
                {matches.map((m, i) => (
                  <tr key={`${m.title}-${m.author}-${m.score}`} style={i === 0 ? { fontWeight: 600 } : {}}>
                    <td>{i + 1}</td>
                    <td>{m.title}</td>
                    <td>{m.author}</td>
                    <td style={{ textAlign: "right" }}>{m.score.toFixed(1)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <ToastContainer
        position="top-center"
        autoClose={4000}
        hideProgressBar={true}
        newestOnTop={false}
        closeOnClick
        rtl={false}
        pauseOnFocusLoss
        pauseOnHover
        theme="light"
        transition={Slide}
      />
    </div>
  );
}

export default App;
