export default function Messages({ error, notice }: { error?: string; notice?: string }) {
  return (
    <>
      {error && <p className="error">{error}</p>}
      {notice && <p className="success">{notice}</p>}
    </>
  );
}
