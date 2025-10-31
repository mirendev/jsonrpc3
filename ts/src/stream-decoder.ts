/**
 * NDJSON (Newline Delimited JSON) stream decoder
 * Each JSON object is separated by a newline character
 */

export class JsonStreamDecoder {
  private buffer = "";
  private decoder = new TextDecoder();

  /**
   * Add data to the buffer and extract complete lines (NDJSON format)
   */
  decode(chunk: Uint8Array): Uint8Array[] {
    // Append chunk to buffer
    this.buffer += this.decoder.decode(chunk, { stream: true });

    const results: Uint8Array[] = [];
    const encoder = new TextEncoder();

    // Split on newlines and process complete lines
    const lines = this.buffer.split("\n");

    // Keep the last incomplete line in the buffer
    this.buffer = lines.pop() || "";

    for (const line of lines) {
      const trimmed = line.trim();
      if (trimmed.length === 0) {
        continue; // Skip empty lines
      }

      // Validate JSON
      try {
        JSON.parse(trimmed);
        results.push(encoder.encode(trimmed));
      } catch (err) {
        throw new Error(`Invalid JSON line: ${err}`);
      }
    }

    return results;
  }
}
