(function () {
  'use strict';

  let index = null;
  let documents = {};

  const searchInput = document.getElementById('search-input');
  const searchResults = document.getElementById('search-results');

  if (!searchInput || !searchResults) return;

  // Load the Zola-generated search index
  fetch('/search_index.en.json')
  .then(response => response.json())
  .then(data => {
    index = elasticlunr.Index.load(data);
  });

  searchInput.addEventListener('input', function () {
    const query = this.value.trim();
    searchResults.innerHTML = '';

    if (!index || query.length < 2) return;

    const results = index.search(query, {
      fields: { title: { boost: 2 }, body: { boost: 1 } },
      expand: true,
    });

    if (results.length === 0) {
      searchResults.innerHTML = '<p class="search-empty">No results found.</p>';
      return;
    }

    const ul = document.createElement('ul');
    ul.className = 'search-results-list';

    results.slice(0, 8).forEach(result => {
  const li = document.createElement('li');
  li.innerHTML = `<a href="${result.ref}">${index.documentStore.getDoc(result.ref).title}</a>`;
  ul.appendChild(li);
});

    searchResults.appendChild(ul);
  });

  // Close results when clicking outside
  document.addEventListener('click', function (e) {
    if (!searchInput.contains(e.target) && !searchResults.contains(e.target)) {
      searchResults.innerHTML = '';
    }
  });
}());
