import React from 'react';

export default function ButtonDocs() {
  return (
    <div className="docs">
      <h1>Button Component</h1>
      <p>A reusable button component for your application.</p>

      <h2>Props</h2>
      <ul>
        <li><code>variant</code>: Button style variant</li>
        <li><code>onClick</code>: Click handler function</li>
      </ul>

      <h2>Examples</h2>
      <pre>
        {`<Button variant="primary">Click me</Button>`}
      </pre>
    </div>
  );
}

