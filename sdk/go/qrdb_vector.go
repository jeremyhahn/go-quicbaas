// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package quicbaas

import (
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// go-qrdb's vector and HNSW service, re-exported so a consumer can drive
// QRDBClient.Vector() (a QRDBVectorClient) without importing go-qrdb.

// Distance metrics and index types of a qrdb vector namespace.
type (
	// QRDBDistanceMetric names a vector distance metric.
	QRDBDistanceMetric = qrdbtransport.DistanceMetric
	// QRDBVectorIndexType names a vector index type.
	QRDBVectorIndexType = qrdbtransport.VectorIndexType
)

// The distance metrics and index types qrdb supports.
const (
	QRDBDistanceCosine     QRDBDistanceMetric  = qrdbtransport.DistanceCosine
	QRDBDistanceEuclidean  QRDBDistanceMetric  = qrdbtransport.DistanceEuclidean
	QRDBDistanceDotProduct QRDBDistanceMetric  = qrdbtransport.DistanceDotProduct
	QRDBIndexHNSW          QRDBVectorIndexType = qrdbtransport.IndexHNSW
	QRDBIndexFlat          QRDBVectorIndexType = qrdbtransport.IndexFlat
	QRDBIndexIVFFlat       QRDBVectorIndexType = qrdbtransport.IndexIVFFlat
)

// Namespaces.
type (
	QRDBCreateNamespaceRequest   = qrdbtransport.CreateNamespaceRequest
	QRDBCreateNamespaceResponse  = qrdbtransport.CreateNamespaceResponse
	QRDBDeleteNamespaceRequest   = qrdbtransport.DeleteNamespaceRequest
	QRDBDeleteNamespaceResponse  = qrdbtransport.DeleteNamespaceResponse
	QRDBListNamespacesRequest    = qrdbtransport.ListNamespacesRequest
	QRDBListNamespacesResponse   = qrdbtransport.ListNamespacesResponse
	QRDBGetNamespaceInfoRequest  = qrdbtransport.GetNamespaceInfoRequest
	QRDBGetNamespaceInfoResponse = qrdbtransport.GetNamespaceInfoResponse
	QRDBNamespaceInfo            = qrdbtransport.NamespaceInfo
	QRDBNamespaceExistsError     = qrdbtransport.NamespaceExistsError
	QRDBNamespaceNotFoundError   = qrdbtransport.NamespaceNotFoundError
)

// Vectors.
type (
	QRDBVectorData                   = qrdbtransport.VectorData
	QRDBVectorNotFoundError          = qrdbtransport.VectorNotFoundError
	QRDBDimensionMismatchError       = qrdbtransport.DimensionMismatchError
	QRDBUpsertVectorRequest          = qrdbtransport.UpsertVectorRequest
	QRDBUpsertVectorResponse         = qrdbtransport.UpsertVectorResponse
	QRDBGetVectorRequest             = qrdbtransport.GetVectorRequest
	QRDBGetVectorResponse            = qrdbtransport.GetVectorResponse
	QRDBDeleteVectorRequest          = qrdbtransport.DeleteVectorRequest
	QRDBDeleteVectorResponse         = qrdbtransport.DeleteVectorResponse
	QRDBBatchUpsertRequest           = qrdbtransport.BatchUpsertRequest
	QRDBBatchUpsertResponse          = qrdbtransport.BatchUpsertResponse
	QRDBBatchDeleteVectorsRequest    = qrdbtransport.BatchDeleteVectorsRequest
	QRDBBatchDeleteVectorsResponse   = qrdbtransport.BatchDeleteVectorsResponse
	QRDBListVectorsRequest           = qrdbtransport.ListVectorsRequest
	QRDBListVectorsResponse          = qrdbtransport.ListVectorsResponse
	QRDBUpdateVectorMetadataRequest  = qrdbtransport.UpdateVectorMetadataRequest
	QRDBUpdateVectorMetadataResponse = qrdbtransport.UpdateVectorMetadataResponse
)

// Search.
type (
	QRDBVectorSearchResult       = qrdbtransport.VectorSearchResult
	QRDBSearchSimilarRequest     = qrdbtransport.SearchSimilarRequest
	QRDBSearchSimilarResponse    = qrdbtransport.SearchSimilarResponse
	QRDBSearchByMetadataRequest  = qrdbtransport.SearchByMetadataRequest
	QRDBSearchByMetadataResponse = qrdbtransport.SearchByMetadataResponse
	QRDBHybridSearchRequest      = qrdbtransport.HybridSearchRequest
	QRDBHybridSearchResponse     = qrdbtransport.HybridSearchResponse
	QRDBSearchByIDRequest        = qrdbtransport.SearchByIDRequest
	QRDBSearchByIDResponse       = qrdbtransport.SearchByIDResponse
)

// Index maintenance.
type (
	QRDBRebuildIndexRequest           = qrdbtransport.RebuildIndexRequest
	QRDBRebuildIndexResponse          = qrdbtransport.RebuildIndexResponse
	QRDBGetRebuildProgressRequest     = qrdbtransport.GetRebuildProgressRequest
	QRDBGetRebuildProgressResponse    = qrdbtransport.GetRebuildProgressResponse
	QRDBGetIndexStatsRequest          = qrdbtransport.GetIndexStatsRequest
	QRDBGetIndexStatsResponse         = qrdbtransport.GetIndexStatsResponse
	QRDBIndexPerformanceMetrics       = qrdbtransport.IndexPerformanceMetrics
	QRDBCompactNamespaceRequest       = qrdbtransport.CompactNamespaceRequest
	QRDBCompactNamespaceResponse      = qrdbtransport.CompactNamespaceResponse
	QRDBGetCompactionProgressRequest  = qrdbtransport.GetCompactionProgressRequest
	QRDBGetCompactionProgressResponse = qrdbtransport.GetCompactionProgressResponse
)
